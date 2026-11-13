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
	t.Parallel()
	b, p, e := taskFixture(t)
	future := time.Now().Add(time.Hour)
	rows := []watch.Watch{
		{ID: "settled", State: "active", RunState: "completed"},
		{ID: "pending", State: "active", RunState: "completed", PendingCount: 1, Pending: []watch.Signal{{Key: "last", Data: json.RawMessage(`{"value":1}`), At: time.Now()}}},
		{ID: "handling", State: "active", RunState: "completed", HandlingTaskID: "finished-task"},
		{ID: "future", State: "active", RunState: "pending", StartAt: &future},
		{ID: "paused", State: "paused", RunState: "paused"},
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

func TestDeletedWatchStopsProcessAndRejectsLateEvents(t *testing.T) {
	b, p, e := taskFixture(t)
	w := watch.Watch{ID: "deleted-watch", OwnerID: p.OwnerID, AssistantID: e.AssistantID, State: "active", RunState: "running", RunID: "watch-run", Version: 1, Pending: []watch.Signal{{Key: "pending", Data: json.RawMessage(`{"value":1}`)}}, Recent: []watch.Signal{{Key: "old"}}}
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, watch.Bucket, persistence.Key(p.OwnerID, w.ID), w)
	}))
	canceled := false
	done := make(chan struct{})
	b.watchRuntime.active[persistence.Key(p.OwnerID, w.ID)] = &watchRun{cancel: func() {
		if !canceled {
			canceled = true
			close(done)
		}
	}, done: done, runID: w.RunID}
	need(t, b.Services.Watches.Delete(t.Context(), p, w.ID, core.Mutation{IdempotencyKey: "delete-watch", ExpectedVersion: 1}))
	need(t, b.watchRuntime.tick(t.Context(), time.Now()))
	if !canceled {
		t.Fatal("deleted watch process survived")
	}
	if _, err := b.Services.Watches.Receive(t.Context(), p.OwnerID, w.ID, w.RunID, []byte(`{"type":"event","key":"late","data":{"x":1}}`)); !persistence.Missing(err) {
		t.Fatal("late observation accepted", err)
	}
	if _, err := b.Services.Watches.Begin(t.Context(), p.OwnerID, w.ID, 2); !persistence.Missing(err) {
		t.Fatal("deleted watch restarted", err)
	}
	stored, err := persistence.Load[watch.Watch](t.Context(), b.store, watch.Bucket, persistence.Key(p.OwnerID, w.ID))
	need(t, err)
	if stored.State != "deleted" || len(stored.Recent) != 1 || len(stored.Pending) != 1 {
		t.Fatal("watch history removed", stored)
	}
}

type deleteAfterReadStore struct {
	persistence.Store
	after func()
}

func (s *deleteAfterReadStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	err := s.Store.Read(ctx, fn)
	if s.after != nil {
		after := s.after
		s.after = nil
		after()
	}
	return err
}
func TestWatchDeletedAfterCandidateReadDoesNotStopScheduler(t *testing.T) {
	b, p, e := taskFixture(t)
	w := watch.Watch{ID: "stale-watch", OwnerID: p.OwnerID, AssistantID: e.AssistantID, State: "active", RunState: "running", RunID: "before-restart", Version: 1}
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, watch.Bucket, persistence.Key(p.OwnerID, w.ID), w)
	}))
	b.watchRuntime.store = &deleteAfterReadStore{Store: b.store, after: func() {
		need(t, b.Services.Watches.Delete(t.Context(), p, w.ID, core.Mutation{IdempotencyKey: "race-delete", ExpectedVersion: 1}))
	}}
	need(t, b.watchRuntime.tick(t.Context(), time.Now()))
	if len(b.watchRuntime.active) != 0 {
		t.Fatal("deleted stale candidate started")
	}
}
