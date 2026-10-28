package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"molr.ai/runtime/internal/business/watch"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type watchTickStore struct {
	persistence.Store
	loaded int
}
type watchTickTx struct {
	persistence.Tx
	parent *watchTickStore
}

func (s *watchTickStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, &watchTickTx{tx, s}) })
}
func (tx *watchTickTx) Query(ctx context.Context, bucket string, query persistence.Query) ([]persistence.Record, error) {
	rows, err := tx.Tx.Query(ctx, bucket, query)
	if bucket == watch.Bucket {
		tx.parent.loaded += len(rows)
	}
	return rows, err
}

func TestWatchTickSkipsSettledHistoryAndDrainsCompletedWork(t *testing.T) {
	b, p, e := taskFixture(t)
	future := time.Now().Add(time.Hour)
	rows := []watch.Watch{
		{ID: "settled", State: "completed"},
		{ID: "pending", State: "completed", PendingCount: 1, Pending: []watch.Signal{{Key: "last", Data: json.RawMessage(`{"value":1}`), At: time.Now()}}},
		{ID: "handling", State: "completed", HandlingTaskID: "finished-task"},
		{ID: "future", State: "pending", StartAt: &future},
		{ID: "paused", State: "paused", Paused: true},
	}
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, w := range rows {
			w.OwnerID, w.AssistantID, w.Version = p.OwnerID, e.AssistantID, 1
			w.Title, w.Instructions = "Watch", "Handle final signal"
			if err := tx.Put(ctx, watch.Bucket, persistence.Key(p.OwnerID, w.ID), w); err != nil {
				return err
			}
		}
		return nil
	}))
	observed := &watchTickStore{Store: b.store}
	b.watchRuntime.store = observed
	canceled := false
	// close() waits for active processes; remove this cancellation probe first.
	defer delete(b.watchRuntime.active, persistence.Key(p.OwnerID, "settled"))
	b.watchRuntime.active[persistence.Key(p.OwnerID, "settled")] = &watchRun{cancel: func() { canceled = true }, done: make(chan struct{}), runID: core.ID("old-run")}
	need(t, b.watchRuntime.tick(t.Context(), time.Now()))
	if observed.loaded != 4 {
		t.Fatalf("loaded settled history: %d watches", observed.loaded)
	}
	if !canceled {
		t.Fatal("completed watcher process was not canceled")
	}
	pending, err := watch.Load(t.Context(), b.store, p.OwnerID, "pending")
	need(t, err)
	if pending.PendingCount != 0 || pending.HandlingTaskID == "" || len(pending.Batch) != 1 {
		t.Fatalf("lost final signal: %+v", pending)
	}
	handling, err := watch.Load(t.Context(), b.store, p.OwnerID, "handling")
	need(t, err)
	if handling.HandlingTaskID != "" {
		t.Fatal("completed task was not settled")
	}
	for _, id := range []core.ID{"future", "paused"} {
		w, err := watch.Load(t.Context(), b.store, p.OwnerID, id)
		need(t, err)
		if w.RunID != "" {
			t.Fatalf("started ineligible watch %s", id)
		}
	}
}
