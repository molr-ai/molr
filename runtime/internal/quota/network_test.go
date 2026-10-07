package quota

import (
	"fmt"
	"molr.ai/runtime/internal/core"
	"sync"
	"testing"
	"time"
)

func TestSharedQuotaConcurrentReservationsAndRestart(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)
	l := &Ledger{Store: openLedgerStore(t, root), Now: func() time.Time { return now }}
	spec := core.NetworkConstraints{Bytes: 1000, Connections: 2, Period: "day", Timezone: "Asia/Shanghai"}
	var wg sync.WaitGroup
	var mu sync.Mutex
	total := int64(0)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			token := fmt.Sprint(i)
			n, err := l.Reserve(t.Context(), "account", token, spec, 100)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			total += n
			mu.Unlock()
			if n > 0 {
				if err := l.Settle(t.Context(), "account", token, n); err != nil {
					t.Error(err)
				}
			}
		}(i)
	}
	wg.Wait()
	if total != 1000 {
		t.Fatal("quota overspent or lost", total)
	}
	l = &Ledger{Store: openLedgerStore(t, root), Now: func() time.Time { return now }}
	u, err := l.Usage(t.Context(), "account", spec)
	if err != nil || u.UsedBytes != 1000 || u.ReservedBytes != 0 {
		t.Fatal(u, err)
	}
	spec.Bytes = 1500
	n, err := l.Reserve(t.Context(), "account", "topup", spec, 1000)
	if err != nil || n != 500 {
		t.Fatal("top-up reset usage", n, err)
	}
	if err := l.Settle(t.Context(), "account", "topup", 100); err != nil {
		t.Fatal(err)
	}
	u, _ = l.Usage(t.Context(), "account", spec)
	if u.UsedBytes != 1100 || u.ReservedBytes != 0 {
		t.Fatal(u)
	}
	now = now.Add(24 * time.Hour)
	u, _ = l.Usage(t.Context(), "account", spec)
	if u.UsedBytes != 0 {
		t.Fatal("period did not advance", u)
	}
}
func TestUnknownOutcomeRetainsCreditAcrossPeriodAndRestart(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	spec := core.NetworkConstraints{Bytes: 100, Connections: 1, Period: "day", Timezone: "UTC"}
	l := &Ledger{Store: openLedgerStore(t, root), Now: func() time.Time { return now }}
	n, err := l.Reserve(t.Context(), "account", "unknown", spec, 80)
	if err != nil || n != 80 {
		t.Fatal(n, err)
	}
	now = now.Add(24 * time.Hour)
	l = &Ledger{Store: openLedgerStore(t, root), Now: func() time.Time { return now }}
	n, err = l.Reserve(t.Context(), "account", "next", spec, 100)
	if err != nil || n != 20 {
		t.Fatal("unknown credit was returned", n, err)
	}
	spec.Timezone = "Asia/Shanghai"
	if _, err = l.Reserve(t.Context(), "account", "zone-reset", spec, 1); err == nil {
		t.Fatal("timezone reset accepted")
	}
}
func TestConnectionSlotsSharedAndReleased(t *testing.T) {
	l := &Ledger{Store: openLedgerStore(t, t.TempDir())}
	if !l.Claim("a", "first", 1) || l.Claim("a", "second", 1) || !l.Claim("b", "second", 1) {
		t.Fatal("incorrect scope")
	}
	l.Release("a", "first")
	if !l.Claim("a", "second", 1) {
		t.Fatal("slot leaked")
	}
}
