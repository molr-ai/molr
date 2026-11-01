package scheduler

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
)

func TestScheduleTickSleepsUntilCommittedChangeOrDeadline(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "scheduler.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := &testkit.IdleStore{Store: db}
	signal := persistence.BucketSignal("test.schedule")
	var ticks atomic.Int64
	var due atomic.Int64
	worker := NewService(store, Options{Handle: func(context.Context, Claim) error { return nil }, Concurrency: 1, TickSignals: []persistence.SignalKey{signal}, Tick: func(context.Context, time.Time) error { ticks.Add(1); return nil }, NextTick: func(context.Context, time.Time) (time.Time, error) {
		n := due.Swap(0)
		if n == 0 {
			return time.Time{}, nil
		}
		return time.Unix(0, n), nil
	}})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	store.RequireIdle(t)
	initial := ticks.Load()
	if initial != 1 {
		t.Fatal("idle tick repeated", initial)
	}
	due.Store(time.Now().Add(100 * time.Millisecond).UnixNano())
	if err := store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return tx.Put(ctx, "test.schedule", "one", true) }); err != nil {
		t.Fatal(err)
	}
	store.RequireIdle(t)
	if ticks.Load() != initial+2 {
		t.Fatal("change or deadline lost", ticks.Load())
	}
}
