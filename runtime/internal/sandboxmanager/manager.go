package sandboxmanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

const operationsBucket = "sandbox.operations"

type record struct {
	Identity  Identity
	Digest    string
	Reply     Reply
	UpdatedAt time.Time
}
type active struct {
	cancel   context.CancelFunc
	lease    time.Time
	identity Identity
}
type Manager struct {
	Backend    Backend
	Store      persistence.Store
	mu         sync.Mutex
	active     map[string]*active
	closed     bool
	closing    chan struct{}
	persistErr error
	wg         sync.WaitGroup
}

func New(ctx context.Context, store persistence.Store, backend Backend) (*Manager, error) {
	m := &Manager{Backend: backend, Store: store, active: map[string]*active{}, closing: make(chan struct{})}
	err := store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		rows, err := tx.Query(ctx, operationsBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("Reply.State", "running")}})
		if err != nil {
			return err
		}
		for _, row := range rows {
			r, err := persistence.Decode[record](row)
			if err != nil {
				return err
			}
			if r.Reply.State == "running" {
				r.Reply = Reply{State: "unknown", Code: core.FailedPrecondition, Reason: "sandbox_outcome_unknown"}
				if err = tx.Put(ctx, operationsBucket, row.Key, r); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return m, err
}
func operationKey(i Identity) string { return persistence.Key(i.OwnerID, i.OperationID) }
func (m *Manager) Submit(ctx context.Context, r Request) (Reply, error) {
	if err := r.Identity.Execution().Validate(); err != nil {
		return Reply{}, err
	}
	if r.Method == "" || len(r.Arguments) > MaxMessage || !json.Valid(r.Arguments) || len(r.Scopes) > 32 || len(r.Networks) > 32 || (r.Method != "shell_execute" && (r.Deadline.IsZero() || r.Deadline.After(time.Now().Add(time.Hour)))) || (!r.Deadline.IsZero() && !r.Deadline.After(time.Now())) {
		return Reply{}, core.Invalid("operation", "invalid sandbox operation")
	}
	// Deadline may shorten on recovery; the immutable intent must remain equal.
	intent := r
	intent.Deadline = time.Time{}
	raw, _ := json.Marshal(intent)
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	key := operationKey(r.Identity)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Reply{}, core.NewError(core.Unavailable, "sandbox closed")
	}
	var out record
	created := false
	err := m.Store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var epoch uint64
		err := tx.Get(ctx, "sandbox.epochs", string(r.Identity.OwnerID), &epoch)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if r.Epoch < epoch {
			return core.NewError(core.Forbidden, "authorization expired")
		}
		err = tx.Get(ctx, operationsBucket, key, &out)
		if err == nil {
			if out.Digest != digest || out.Identity != r.Identity {
				return core.NewError(core.Conflict, "operation identity reused")
			}
			return nil
		}
		if !persistence.Missing(err) {
			return err
		}
		if len(m.active) >= 16 {
			return core.NewError(core.RateLimited, "sandbox execution limit")
		}
		out = record{Identity: r.Identity, Digest: digest, Reply: Reply{State: "running"}, UpdatedAt: time.Now()}
		created = true
		return tx.Put(ctx, operationsBucket, key, out)
	})
	if err != nil {
		return Reply{}, err
	}
	if !created {
		return out.Reply, nil
	}
	run, cancel := context.WithCancel(context.Background())
	if !r.Deadline.IsZero() {
		cancel()
		run, cancel = context.WithDeadline(context.Background(), r.Deadline)
	}
	a := &active{cancel: cancel, lease: time.Now().Add(LeaseDuration), identity: r.Identity}
	m.active[key] = a
	m.wg.Add(1)
	go func(out record) {
		defer m.wg.Done()
		defer cancel()
		done := make(chan struct{})
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-run.Done():
					return
				case <-ticker.C:
					m.mu.Lock()
					expired := time.Now().After(a.lease)
					m.mu.Unlock()
					if expired {
						cancel()
						return
					}
				}
			}
		}()
		result, err := m.Backend.Call(run, r)
		close(done)
		reply := Reply{State: "succeeded", Result: result}
		if err != nil {
			reply = Reply{State: "unknown", Code: core.CodeOf(err), Reason: core.ReasonOf(err)}
			if r.Method == "file_search" || r.Method == "file_read" || r.Method == "file_list" || r.Method == "context" || r.Method == "download" || r.Method == "snapshot" || core.CodeOf(err) == core.InvalidArgument || core.CodeOf(err) == core.Forbidden || core.CodeOf(err) == core.NotFound || core.CodeOf(err) == core.Conflict {
				reply.State = "failed"
			}
		}
		if err == nil && (len(result) > MaxMessage || !json.Valid(result)) {
			reply = Reply{State: "unknown", Code: core.Internal, Reason: "sandbox_invalid_result"}
		}
		out.Reply = reply
		out.UpdatedAt = time.Now()
		err = m.persistResult(key, out)
		m.mu.Lock()
		m.persistErr = errors.Join(m.persistErr, err)
		delete(m.active, key)
		m.mu.Unlock()
	}(out)
	return out.Reply, nil
}

// Keep the execution slot and completed reply until durable. Retrying this
// transaction must never repeat the backend's external effect.
func (m *Manager) persistResult(key string, out record) error {
	delay := 100 * time.Millisecond
	for {
		persist, stop := context.WithTimeout(context.Background(), 5*time.Second)
		err := m.Store.Write(persist, func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, operationsBucket, key, out)
		})
		stop()
		if err == nil {
			return nil
		}
		if core.CodeOf(err) == core.InvalidArgument && len(out.Reply.Result) > 0 {
			// A valid protocol result may exceed the durable record limit after
			// JSON encoding. Persist an explicit unknown outcome instead.
			out.Reply = Reply{State: "unknown", Code: core.Internal, Reason: "sandbox_invalid_result"}
			continue
		}
		timer := time.NewTimer(delay)
		select {
		case <-m.closing:
			timer.Stop()
			return err
		case <-timer.C:
		}
		if delay < time.Second {
			delay = min(delay*2, time.Second)
		}
	}
}
func (m *Manager) Get(ctx context.Context, i Identity) (Reply, error) {
	if err := i.Execution().Validate(); err != nil {
		return Reply{}, err
	}
	r, err := persistence.Load[record](ctx, m.Store, operationsBucket, operationKey(i))
	if err != nil {
		return Reply{}, err
	}
	if r.Identity != i {
		return Reply{}, core.NewError(core.Forbidden, "operation identity mismatch")
	}
	return r.Reply, nil
}
func (m *Manager) Renew(ctx context.Context, i Identity) error {
	if _, err := m.Get(ctx, i); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if a := m.active[operationKey(i)]; a != nil {
		a.lease = time.Now().Add(LeaseDuration)
	}
	return nil
}
func (m *Manager) Cancel(ctx context.Context, i Identity) error {
	if _, err := m.Get(ctx, i); err != nil {
		return err
	}
	m.mu.Lock()
	if a := m.active[operationKey(i)]; a != nil {
		a.cancel()
	}
	m.mu.Unlock()
	return m.Backend.Cancel(ctx, i)
}
func (m *Manager) Revoke(ctx context.Context, owner core.ID, epoch uint64) error {
	if owner.Validate() != nil {
		return core.Invalid("owner", "invalid")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	err := m.Store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var old uint64
		err := tx.Get(ctx, "sandbox.epochs", string(owner), &old)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if epoch < old {
			return core.NewError(core.Conflict, "stale authorization epoch")
		}
		return tx.Put(ctx, "sandbox.epochs", string(owner), epoch)
	})
	if err != nil {
		return err
	}
	for _, a := range m.active {
		if a.identity.OwnerID == owner {
			a.cancel()
		}
	}
	return nil
}
func (m *Manager) Close() error {
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		close(m.closing)
	}
	for _, a := range m.active {
		a.cancel()
	}
	m.mu.Unlock()
	err := m.Backend.Close()
	m.wg.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	return errors.Join(err, m.persistErr)
}

// Read avoids storing large transient blobs and context scans in the operation
// ledger. Mutations must always go through Submit.
func (m *Manager) Read(ctx context.Context, q Request) (json.RawMessage, error) {
	if err := q.Identity.Execution().Validate(); err != nil {
		return nil, err
	}
	if q.Method != "snapshot" && q.Method != "download" && q.Method != "shell_read" && q.Method != "shell_output" && q.Method != "quota_usage" {
		return nil, core.Invalid("method", "read-only method required")
	}
	if len(q.Scopes) > 32 || len(q.Arguments) > MaxMessage || !q.Deadline.After(time.Now()) || q.Deadline.After(time.Now().Add(time.Hour)) {
		return nil, core.Invalid("request", "invalid read")
	}
	epoch, err := persistence.Load[uint64](ctx, m.Store, "sandbox.epochs", string(q.Identity.OwnerID))
	if err != nil && !persistence.Missing(err) {
		return nil, err
	}
	if q.Epoch < epoch {
		return nil, core.NewError(core.Forbidden, "authorization expired")
	}
	ctx, cancel := context.WithDeadline(ctx, q.Deadline)
	defer cancel()
	return m.Backend.Call(ctx, q)
}
