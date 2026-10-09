package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

type interruptReadStore struct {
	persistence.Store
	reads     atomic.Int64
	afterRead func(int64)
	failFirst bool
}

func (s *interruptReadStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	n := s.reads.Add(1)
	if s.failFirst && n == 1 {
		return core.NewError(core.Unavailable, "temporary read failure")
	}
	err := s.Store.Read(ctx, fn)
	if s.afterRead != nil {
		s.afterRead(n)
	}
	return err
}
func interruptFixture(t *testing.T) (*Engine, *interruptReadStore, *sqlitestore.Store, Turn) {
	t.Helper()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "interrupt.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store := &interruptReadStore{Store: db}
	return &Engine{store: store, options: Options{Now: time.Now}}, store, db, Turn{Principal: core.Principal{OwnerID: "owner"}, Event: event.AssistantEvent{ID: "active", AssistantID: "assistant"}}
}
func startInterruptWatch(t *testing.T, e *Engine, turn Turn) (context.Context, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancelCause(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); e.watchInterrupts(ctx, turn, nil, cancel) }()
	t.Cleanup(func() { cancel(nil); awaitExpedite(t, done) })
	return ctx, done
}
func interruptJob(id string, at time.Time) event.Job {
	return event.Job{Event: event.AssistantEvent{ID: core.ID(id), OwnerID: "owner", AssistantID: "assistant", Kind: event.ShellTaskFinished, OperationID: core.ID(id), DeliveryMode: event.Interrupt, CreatedAt: time.Now()}, State: event.Queued, NotBefore: at, Version: 1}
}
func saveInterruptJob(t *testing.T, db persistence.Store, job event.Job) {
	t.Helper()
	if err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return event.SaveJob(ctx, tx, job) }); err != nil {
		t.Fatal(err)
	}
}
func TestIdleInterruptReadCount(t *testing.T) {
	e, store, db, turn := interruptFixture(t)
	ready := make(chan struct{})
	store.afterRead = func(n int64) {
		if n == 3 {
			close(ready)
		}
	}
	ctx, _ := startInterruptWatch(t, e, turn)
	awaitExpedite(t, ready)
	for i := 0; i < 20; i++ {
		job := interruptJob(fmt.Sprint(i), time.Now())
		if i%2 == 0 {
			job.Event.OwnerID = "other-owner"
		} else {
			job.Event.AssistantID = "other-assistant"
		}
		saveInterruptJob(t, db, job)
	}
	time.Sleep(550 * time.Millisecond)
	if n := store.reads.Load(); n != 3 {
		t.Fatalf("idle watcher queried again: %d reads", n)
	}
	if ctx.Err() != nil {
		t.Fatal(context.Cause(ctx))
	}
	t.Log("idle window 550ms with 20 unrelated commits: 3 initial read transactions, no repeated reads")
}
func TestInterruptCommitDuringReadIsNotLost(t *testing.T) {
	e, store, db, turn := interruptFixture(t)
	queried, release := make(chan struct{}), make(chan struct{})
	store.afterRead = func(n int64) {
		if n == 3 {
			close(queried)
			<-release
		}
	}
	ctx, done := startInterruptWatch(t, e, turn)
	// The first inbox snapshot is already empty. Commit before the watcher waits.
	awaitExpedite(t, queried)
	saveInterruptJob(t, db, interruptJob("during-read", time.Now()))
	close(release)
	awaitExpedite(t, done)
	if context.Cause(ctx) != errExpedited {
		t.Fatal(context.Cause(ctx))
	}
}
func TestInterruptFutureDeadlineWithoutAnotherCommit(t *testing.T) {
	e, store, db, turn := interruptFixture(t)
	due := time.Now().Add(300 * time.Millisecond)
	saveInterruptJob(t, db, interruptJob("future", due))
	ctx, done := startInterruptWatch(t, e, turn)
	awaitExpedite(t, done)
	if context.Cause(ctx) != errExpedited || time.Now().Before(due) {
		t.Fatal("wrong deadline", context.Cause(ctx))
	}
	if n := store.reads.Load(); n != 6 {
		t.Fatalf("expected initial and due reads, got %d", n)
	}
}
func TestInterruptRollbackDoesNotWakeAndCommitDoes(t *testing.T) {
	e, store, db, turn := interruptFixture(t)
	ready := make(chan struct{})
	store.afterRead = func(n int64) {
		if n == 3 {
			close(ready)
		}
	}
	ctx, done := startInterruptWatch(t, e, turn)
	awaitExpedite(t, ready)
	rollback := errors.New("rollback")
	err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := event.SaveJob(ctx, tx, interruptJob("rolled-back", time.Now())); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if store.reads.Load() != 3 || ctx.Err() != nil {
		t.Fatal("rolled back input woke model")
	}
	saveInterruptJob(t, db, interruptJob("committed", time.Now()))
	awaitExpedite(t, done)
	if context.Cause(ctx) != errExpedited {
		t.Fatal(context.Cause(ctx))
	}
}
func TestInterruptRetriesReadFailureWithoutNewWrite(t *testing.T) {
	e, store, db, turn := interruptFixture(t)
	saveInterruptJob(t, db, interruptJob("pending", time.Now()))
	store.failFirst = true
	ctx, done := startInterruptWatch(t, e, turn)
	awaitExpedite(t, done)
	if context.Cause(ctx) != errExpedited {
		t.Fatal(context.Cause(ctx))
	}
}
func TestInterruptStoreCloseAndDreamCleanup(t *testing.T) {
	t.Run("closed store", func(t *testing.T) {
		e, store, db, turn := interruptFixture(t)
		ready := make(chan struct{})
		store.afterRead = func(n int64) {
			if n == 3 {
				close(ready)
			}
		}
		ctx, done := startInterruptWatch(t, e, turn)
		awaitExpedite(t, ready)
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		awaitExpedite(t, done)
		if core.CodeOf(context.Cause(ctx)) != core.Unavailable {
			t.Fatal(context.Cause(ctx))
		}
	})
	t.Run("dream", func(t *testing.T) {
		e, store, _, turn := interruptFixture(t)
		turn.Event.Kind = event.DreamDue
		ctx, done := startInterruptWatch(t, e, turn)
		awaitExpedite(t, done)
		if ctx.Err() != nil || store.reads.Load() != 0 {
			t.Fatal("dream monitored ordinary inputs")
		}
	})
}
