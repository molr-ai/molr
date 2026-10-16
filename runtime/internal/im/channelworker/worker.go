// Package channelworker schedules channel connections and durable work from
// committed changes and explicit deadlines. It never polls idle collections.
package channelworker

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"sync"
	"time"
)

type Binding struct {
	ID, OwnerID, AssistantID, ConversationID core.ID
	Connect, Work                            bool
	Expires                                  time.Time
}
type Options[B any] struct {
	ConnectionKey func(B) string
	connections   *sync.Map
	Store         persistence.Store
	Bucket        string
	Describe      func(B) Binding
	Live          func(context.Context, core.ID, core.ID) error
	LiveSignals   func(core.ID, core.ID) []persistence.SignalKey
	Connect       func(context.Context, B)
	Step          func(context.Context, B) (time.Time, error)
	Expire        func(context.Context, B) error
}

func bindingSignal(bucket string, owner, chat core.ID) persistence.SignalKey {
	return persistence.SignalKey{Namespace: bucket, Key: persistence.Key(owner, chat)}
}
func pendingSignal(bucket string) persistence.SignalKey {
	return persistence.SignalKey{Namespace: bucket + ".pending", Key: "pending"}
}
func workSignal(bucket string, id core.ID) persistence.SignalKey {
	return persistence.SignalKey{Namespace: bucket + ".work", Key: string(id)}
}

// Changed records discovery and scoped invalidation in the caller's transaction.
// Multiple changes to one conversation coalesce; rollback discards both effects.
func Changed(ctx context.Context, tx persistence.Tx, bucket string, owner, chat core.ID) error {
	if err := tx.Put(ctx, bucket+".pending", persistence.Key(owner, chat), Binding{OwnerID: owner, ConversationID: chat}); err != nil {
		return err
	}
	if err := tx.NotifyAfterCommit(ctx, bindingSignal(bucket, owner, chat)); err != nil {
		return err
	}
	return tx.NotifyAfterCommit(ctx, pendingSignal(bucket))
}

// Wake schedules work only for the affected binding after its transaction commits.
func Wake(ctx context.Context, tx persistence.Tx, bucket string, id core.ID) error {
	return tx.NotifyAfterCommit(ctx, workSignal(bucket, id))
}

func initial[B any](o Options[B], ctx context.Context) ([]B, error) {
	return persistence.Scan[B](ctx, o.Store, o.Bucket)
}
func pending[B any](o Options[B], ctx context.Context) (rows []Binding, err error) {
	err = o.Store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var err error
		rows, err = persistence.Scan[Binding](ctx, tx.Store(), o.Bucket+".pending")
		if err != nil {
			return err
		}
		for _, b := range rows {
			if err = tx.Delete(ctx, o.Bucket+".pending", persistence.Key(b.OwnerID, b.ConversationID)); err != nil {
				return err
			}
		}
		return nil
	})
	return
}

// Run restores a single startup snapshot, then consumes only changed conversations.
// There must be one Run per channel and store. All children are joined on return.
func Run[B any](parent context.Context, o Options[B]) error {
	if o.connections == nil {
		o.connections = &sync.Map{}
	}
	ctx, cancel := context.WithCancel(parent)
	var wg sync.WaitGroup
	defer func() { cancel(); wg.Wait() }()
	watch, err := o.Store.Watch(pendingSignal(o.Bucket))
	if err != nil {
		return err
	}
	defer watch.Close()
	rows, err := initial(o, ctx)
	if err != nil {
		return err
	}
	type completion struct {
		b   Binding
		err error
	}
	done := make(chan completion)
	running := map[string]bool{}
	start := func(ref Binding) error {
		key := persistence.Key(ref.OwnerID, ref.ConversationID)
		if running[key] {
			return nil
		}
		b, err := persistence.Load[B](ctx, o.Store, o.Bucket, key)
		if persistence.Missing(err) {
			return nil
		}
		if err != nil {
			return err
		}
		ref = o.Describe(b)
		running[key] = true
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := observe(ctx, o, ref)
			select {
			case done <- completion{ref, err}:
			case <-ctx.Done():
			}
		}()
		return nil
	}
	for _, b := range rows {
		if err = start(o.Describe(b)); err != nil {
			return err
		}
	}
	for {
		refs, err := pending(o, ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		for _, ref := range refs {
			if err = start(ref); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-watch.Done():
			return core.NewError(core.Unavailable, "channel store closed")
		case <-watch.Changes():
		case result := <-done:
			delete(running, persistence.Key(result.b.OwnerID, result.b.ConversationID))
			if result.err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return result.err
			}
			// Replacement can commit after an observer sees deletion but before it exits.
			if err = start(result.b); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}

type signals struct {
	changed chan struct{}
	ctx     context.Context
	close   func()
}

func subscribe(ctx context.Context, store persistence.Store, keys []persistence.SignalKey) (signals, error) {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	s := signals{changed: make(chan struct{}, 1), ctx: ctx}
	s.close = func() { cancel(); wg.Wait() }
	for _, key := range keys {
		w, err := store.Watch(key)
		if err != nil {
			s.close()
			return s, err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer w.Close()
			for {
				select {
				case <-ctx.Done():
					return
				case <-w.Done():
					cancel()
					return
				case <-w.Changes():
					select {
					case s.changed <- struct{}{}:
					default:
					}
				}
			}
		}()
	}
	return s, nil
}

type task struct {
	cancel     context.CancelFunc
	done       chan struct{}
	generation core.ID
}

func (t *task) stop() {
	if t.cancel != nil {
		t.cancel()
		<-t.done
		t.cancel = nil
		t.done = nil
	}
}
func (t *task) start(ctx context.Context, id core.ID, fn func(context.Context)) {
	ctx, t.cancel = context.WithCancel(ctx)
	t.generation = id
	t.done = make(chan struct{})
	done := t.done
	go func() { defer close(done); fn(ctx) }()
}
func observe[B any](ctx context.Context, o Options[B], ref Binding) error {
	keys := []persistence.SignalKey{bindingSignal(o.Bucket, ref.OwnerID, ref.ConversationID)}
	if o.LiveSignals != nil {
		keys = append(keys, o.LiveSignals(ref.OwnerID, ref.AssistantID)...)
	}
	changes, err := subscribe(ctx, o.Store, keys)
	if err != nil {
		return err
	}
	defer changes.close()
	var connection, work task
	defer connection.stop()
	defer work.stop()
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	for {
		b, err := persistence.Load[B](ctx, o.Store, o.Bucket, persistence.Key(ref.OwnerID, ref.ConversationID))
		if persistence.Missing(err) {
			return nil
		}
		if err != nil {
			return err
		}
		info := o.Describe(b)
		if !info.Expires.IsZero() && !time.Now().Before(info.Expires) {
			if err = o.Expire(ctx, b); err != nil {
				return err
			}
			continue
		}
		live := o.Live(ctx, info.OwnerID, info.AssistantID) == nil
		if connection.cancel != nil && (!live || !info.Connect || connection.generation != info.ID) {
			connection.stop()
		}
		if work.cancel != nil && (!live || !info.Work || work.generation != info.ID) {
			work.stop()
		}
		if live && info.Connect && connection.cancel == nil {
			connection.start(ctx, info.ID, func(ctx context.Context) {
				if o.ConnectionKey != nil {
					key := o.ConnectionKey(b)
					value, _ := o.connections.LoadOrStore(key, make(chan struct{}, 1))
					gate := value.(chan struct{})
					select {
					case gate <- struct{}{}:
						defer func() { <-gate }()
					case <-ctx.Done():
						return
					}
					if ctx.Err() != nil {
						return
					}
				}
				o.Connect(ctx, b)
			})
		}
		if live && info.Work && work.cancel == nil {
			work.start(ctx, info.ID, func(ctx context.Context) { runWork(ctx, o, info) })
		}
		timer.Stop()
		var expiry <-chan time.Time
		if !info.Expires.IsZero() {
			timer.Reset(time.Until(info.Expires))
			expiry = timer.C
		}
		select {
		case <-ctx.Done():
			return nil
		case <-changes.ctx.Done():
			return core.NewError(core.Unavailable, "channel watch closed")
		case <-changes.changed:
		case <-expiry:
		case <-work.done:
			work.stop()
		case <-connection.done:
			connection.stop()
			// A completed network loop normally committed a terminal state. Re-read it;
			// if no state changed, avoid an unbounded immediate reconnect loop.
			select {
			case <-ctx.Done():
				return nil
			case <-changes.changed:
			case <-time.After(time.Second):
			}
		}
	}
}
func runWork[B any](ctx context.Context, o Options[B], ref Binding) {
	changes, err := subscribe(ctx, o.Store, []persistence.SignalKey{bindingSignal(o.Bucket, ref.OwnerID, ref.ConversationID), workSignal(o.Bucket, ref.ID)})
	if err != nil {
		return
	}
	defer changes.close()
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	backoff := time.Second
	for ctx.Err() == nil {
		var deadline time.Time
		b, err := persistence.Load[B](ctx, o.Store, o.Bucket, persistence.Key(ref.OwnerID, ref.ConversationID))
		if persistence.Missing(err) {
			return
		}
		if err == nil {
			info := o.Describe(b)
			if info.ID != ref.ID || !info.Work || o.Live(ctx, info.OwnerID, info.AssistantID) != nil {
				return
			}
			deadline, err = o.Step(ctx, b)
		}
		if err != nil {
			deadline = time.Now().Add(backoff)
			backoff = min(backoff*2, time.Minute)
		} else {
			backoff = time.Second
		}
		timer.Stop()
		var due <-chan time.Time
		if !deadline.IsZero() {
			timer.Reset(time.Until(deadline))
			due = timer.C
		}
		select {
		case <-ctx.Done():
			return
		case <-changes.ctx.Done():
			return
		case <-changes.changed:
		case <-due:
		}
	}
}
