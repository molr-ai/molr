// Network accounts are
// keyed by owner/assistant/stable sandbox source, never a replaceable grant ID.
package quota

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/persistence"
	"strings"
	"sync"
	"time"

	"molr.ai/runtime/internal/core"
)

type NetworkUsage struct {
	UsedBytes     int64
	ReservedBytes int64
	LimitBytes    int64
	PeriodStart   time.Time
	PeriodEnd     time.Time
	Connections   int
}
type reservation struct {
	Bytes  int64
	Period string
}
type period struct {
	Used     int64
	Reserved int64
}
type account struct {
	Period       string
	Timezone     string
	Periods      map[string]period
	Reservations map[string]reservation
}
type Ledger struct {
	Store persistence.Store
	Now   func() time.Time
	mu    sync.Mutex
	slots map[string]map[string]bool
}

type networkTxn struct {
	tx  persistence.Tx
	ctx context.Context
	Now func() time.Time
}

func (l *networkTxn) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}
func (l *networkTxn) load(key string) (account, error) {
	a := account{Periods: map[string]period{}, Reservations: map[string]reservation{}}
	err := l.tx.Get(l.ctx, "quota.network.accounts", key, &a)
	if persistence.Missing(err) {
		err = nil
	}
	return a, err
}
func (l *networkTxn) save(key string, a account) error {
	return l.tx.Put(l.ctx, "quota.network.accounts", key, a)
}
func (l *Ledger) write(ctx context.Context, fn func(*networkTxn) error) error {
	if l.Store == nil {
		return core.NewError(core.Unavailable, "quota store unavailable")
	}
	return l.Store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return fn(&networkTxn{tx: tx, ctx: ctx, Now: l.Now})
	})
}
func (l *Ledger) Reserve(ctx context.Context, key, token string, spec core.NetworkConstraints, wanted int64) (int64, error) {
	var n int64
	err := l.write(ctx, func(tx *networkTxn) error { var err error; n, err = tx.reserve(key, token, spec, wanted); return err })
	if err != nil {
		return 0, err
	}
	return n, nil
}
func (l *networkTxn) reserve(key, token string, spec core.NetworkConstraints, wanted int64) (int64, error) {
	if wanted < 1 || wanted > 1<<20 || token == "" {
		return 0, fmt.Errorf("invalid reservation")
	}
	a, err := l.load(key)
	if err != nil {
		return 0, err
	}
	if a.Period != "" && (a.Period != spec.Period || a.Timezone != spec.Timezone) {
		if !strings.Contains(key, "/group/") {
			return 0, fmt.Errorf("quota period cannot reset an existing account")
		}
		if err := l.rebaseGroup(&a, spec); err != nil {
			return 0, err
		}
	}
	a.Period, a.Timezone = spec.Period, spec.Timezone
	if _, exists := a.Reservations[token]; exists {
		return 0, fmt.Errorf("reservation token reused")
	}
	start, _ := Period(spec.Period, spec.Timezone, l.now())
	id := start.Format(time.RFC3339)
	p := a.Periods[id]
	// Unknown reservations from prior periods still occupy capacity until reconciled.
	held := int64(0)
	for k, v := range a.Periods {
		if k != id {
			held += v.Reserved
		}
	}
	left := spec.Bytes - p.Used - p.Reserved - held
	if left <= 0 {
		return 0, nil
	}
	n := min(wanted, left)
	p.Reserved += n
	a.Periods[id] = p
	a.Reservations[token] = reservation{n, id}
	return n, l.save(key, a)
}
func (l *Ledger) Settle(ctx context.Context, key, token string, used int64) error {
	return l.write(ctx, func(tx *networkTxn) error { return tx.settle(key, token, used) })
}
func (l *networkTxn) settle(key, token string, used int64) error {
	a, err := l.load(key)
	if err != nil {
		return err
	}
	r, ok := a.Reservations[token]
	if !ok {
		return fmt.Errorf("unknown reservation")
	}
	if used < 0 || used > r.Bytes {
		return fmt.Errorf("invalid settlement")
	}
	p := a.Periods[r.Period]
	p.Reserved -= r.Bytes
	p.Used += used
	a.Periods[r.Period] = p
	delete(a.Reservations, token)
	return l.save(key, a)
}
func (l *Ledger) Claim(key, slot string, limit int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.claim(key, slot, limit)
}
func (l *Ledger) claim(key, slot string, limit int) bool {
	if l.slots == nil {
		l.slots = map[string]map[string]bool{}
	}
	if l.slots[key] == nil {
		l.slots[key] = map[string]bool{}
	}
	if l.slots[key][slot] {
		return true
	}
	if len(l.slots[key]) >= limit {
		return false
	}
	l.slots[key][slot] = true
	return true
}
func (l *Ledger) Release(key, slot string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.slots[key], slot)
}
func (l *Ledger) Usage(ctx context.Context, key string, spec core.NetworkConstraints) (NetworkUsage, error) {
	if l.Store == nil {
		return NetworkUsage{}, core.NewError(core.Unavailable, "quota store unavailable")
	}
	var out NetworkUsage
	err := l.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var err error
		out, err = (&networkTxn{tx: tx, ctx: ctx, Now: l.Now}).usage(key, spec)
		return err
	})
	l.mu.Lock()
	out.Connections = len(l.slots[key])
	l.mu.Unlock()
	return out, err
}
func (l *networkTxn) usage(key string, spec core.NetworkConstraints) (NetworkUsage, error) {
	a, err := l.load(key)
	if err != nil {
		return NetworkUsage{}, err
	}
	if strings.Contains(key, "/group/") && a.Period != "" && (a.Period != spec.Period || a.Timezone != spec.Timezone) {
		if err := l.rebaseGroup(&a, spec); err != nil {
			return NetworkUsage{}, err
		}
	}
	start, end := Period(spec.Period, spec.Timezone, l.now())
	p := a.Periods[start.Format(time.RFC3339)]
	reserved := int64(0)
	for _, v := range a.Periods {
		reserved += v.Reserved
	}
	return NetworkUsage{UsedBytes: p.Used, ReservedBytes: reserved, LimitBytes: spec.Bytes, PeriodStart: start, PeriodEnd: end}, nil
}

type AccountLimit struct {
	Key   string
	Limit core.NetworkConstraints
}

// ReserveMany commits every affected account in one database transaction.
func (l *Ledger) ReserveMany(ctx context.Context, accounts []AccountLimit, token string, wanted int64) (int64, error) {
	if len(accounts) == 0 || wanted < 1 || wanted > 1<<20 || token == "" {
		return 0, fmt.Errorf("invalid reservation")
	}
	n := wanted
	err := l.write(ctx, func(tx *networkTxn) error {
		seen := map[string]bool{}
		for _, a := range accounts {
			if seen[a.Key] {
				return fmt.Errorf("duplicate quota account")
			}
			seen[a.Key] = true
			v, err := tx.usage(a.Key, a.Limit)
			if err != nil {
				return err
			}
			n = min(n, max(int64(0), a.Limit.Bytes-v.UsedBytes-v.ReservedBytes))
		}
		if n == 0 {
			return nil
		}
		for _, a := range accounts {
			used, err := tx.reserve(a.Key, token, a.Limit, n)
			if err != nil {
				return err
			}
			if used != n {
				return fmt.Errorf("quota changed during reservation")
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

// SettleMany releases or charges all layers atomically; failed commits keep holds.
func (l *Ledger) SettleMany(ctx context.Context, keys []string, token string, used int64) error {
	if len(keys) == 0 {
		return fmt.Errorf("quota accounts required")
	}
	return l.write(ctx, func(tx *networkTxn) error {
		for _, key := range keys {
			if err := tx.settle(key, token, used); err != nil {
				return err
			}
		}
		return nil
	})
}
func (l *Ledger) ClaimMany(accounts []AccountLimit, slot string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	claimed := []string{}
	for _, a := range accounts {
		if !l.claim(a.Key, slot, a.Limit.Connections) {
			for _, key := range claimed {
				delete(l.slots[key], slot)
			}
			return false
		}
		claimed = append(claimed, a.Key)
	}
	return len(claimed) > 0
}

// Changes to group membership or its calendar must not erase consumed traffic.
// Old buckets overlapping the new window are charged conservatively in full;
// pending reservations retain their settlement tokens in the new window.
func (l *networkTxn) rebaseGroup(a *account, spec core.NetworkConstraints) error {
	start, end := Period(spec.Period, spec.Timezone, l.now())
	used := int64(0)
	oldSpec := core.NetworkConstraints{Period: a.Period, Timezone: a.Timezone}
	for id, v := range a.Periods {
		at, err := time.Parse(time.RFC3339, id)
		if err != nil {
			return err
		}
		oldStart, oldEnd := Period(oldSpec.Period, oldSpec.Timezone, at)
		if (end.IsZero() || oldStart.Before(end)) && (oldEnd.IsZero() || oldEnd.After(start)) {
			used += v.Used
		}
		v.Used = 0
		a.Periods[id] = v
	}
	id := start.Format(time.RFC3339)
	reserved := int64(0)
	for token, r := range a.Reservations {
		reserved += r.Bytes
		r.Period = id
		a.Reservations[token] = r
	}
	a.Periods = map[string]period{id: {Used: used, Reserved: reserved}}
	a.Period, a.Timezone = spec.Period, spec.Timezone
	return nil
}
