package scheduler

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

type boundaryReadStore struct {
	persistence.Store
	reads   int
	byKey   map[string]int
	failKey string
	failure error
}
type boundaryReadTx struct {
	persistence.Tx
	counts *boundaryReadStore
}

func (s *boundaryReadStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, &boundaryReadTx{tx, s}) })
}
func (tx *boundaryReadTx) Get(ctx context.Context, bucket, key string, value any) error {
	if bucket == "agent.interrupt_boundaries" {
		tx.counts.reads++
		if tx.counts.byKey == nil {
			tx.counts.byKey = map[string]int{}
		}
		tx.counts.byKey[key]++
		if key == tx.counts.failKey {
			return tx.counts.failure
		}
	}
	return tx.Tx.Get(ctx, bucket, key, value)
}

func TestJobListReadsEachAssistantBoundaryOnce(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "agent.interrupt_boundaries", "owner/assistant", event.InterruptBoundary{At: now, ConversationID: "chat", ThroughSequence: 120}); err != nil {
			return err
		}
		for i := 0; i < 120; i++ {
			id := core.ID(fmt.Sprintf("job-%03d", i))
			job := Job{State: Queued, Version: 1, Event: event.AssistantEvent{ID: id, OwnerID: "owner", AssistantID: "assistant", Kind: event.UserMessage, MessageID: core.ID(fmt.Sprintf("message-%03d", i)), MessageSequence: uint64(i + 1), CreatedAt: now.Add(-time.Minute)}}
			if err := tx.Put(ctx, jobsBucket, persistence.Key("owner", id), job); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	counts := &boundaryReadStore{Store: db}
	page, err := NewQueue(counts).List(t.Context(), "owner", Query{AssistantID: "assistant", States: []State{Canceled}, Page: core.PageRequest{Limit: 100}})
	if err != nil || len(page.Items) != 100 || page.NextCursor == "" {
		t.Fatalf("incorrect canceled page: size=%d cursor=%t error=%v", len(page.Items), page.NextCursor != "", err)
	}
	for _, job := range page.Items {
		if job.State != Canceled {
			t.Fatal("stopped work remains visible as queued")
		}
	}
	t.Logf("returned jobs=%d boundary reads=%d", len(page.Items), counts.reads)
	if counts.reads != 1 {
		t.Fatalf("read the same assistant boundary %d times for one page", counts.reads)
	}
}

func TestJobListBoundaryMissingRefreshAndFilteredPages(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "refresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < 250; i++ {
			id := core.ID(fmt.Sprintf("job-%03d", i))
			if err := tx.Put(ctx, jobsBucket, persistence.Key("owner", id), Job{State: Queued, Version: 1, Event: event.AssistantEvent{ID: id, OwnerID: "owner", AssistantID: "assistant", Kind: event.UserMessage, MessageSequence: uint64(i + 1), CreatedAt: now.Add(-time.Minute)}}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	counts := &boundaryReadStore{Store: db}
	q := NewQueue(counts)
	canceled := Query{AssistantID: "assistant", States: []State{Canceled}, Page: core.PageRequest{Limit: 100}}
	page, err := q.List(t.Context(), "owner", canceled)
	if err != nil || len(page.Items) != 0 || counts.reads != 1 {
		t.Fatalf("missing boundary: jobs=%d reads=%d err=%v", len(page.Items), counts.reads, err)
	}
	if err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "agent.interrupt_boundaries", "owner/assistant", event.InterruptBoundary{At: now, ConversationID: "chat", ThroughSequence: 240})
	}); err != nil {
		t.Fatal(err)
	}
	counts.reads = 0
	page, err = q.List(t.Context(), "owner", Query{AssistantID: "assistant", States: []State{Queued}, Page: core.PageRequest{Limit: 5}})
	if err != nil || len(page.Items) != 5 || page.Items[0].Event.MessageSequence != 241 || page.NextCursor == "" || counts.reads != 1 {
		t.Fatalf("filtered page missed current boundary: jobs=%d reads=%d err=%v", len(page.Items), counts.reads, err)
	}
	counts.reads = 0
	next, err := q.List(t.Context(), "owner", Query{AssistantID: "assistant", States: []State{Queued}, Page: core.PageRequest{Limit: 5, Cursor: page.NextCursor}})
	if err != nil || len(next.Items) != 5 || next.Items[0].Event.MessageSequence != 246 || next.NextCursor != "" || counts.reads != 1 {
		t.Fatalf("next page: jobs=%d reads=%d err=%v", len(next.Items), counts.reads, err)
	}
	// Removing a boundary must affect the next read and the separate Cancel write.
	if err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Delete(ctx, "agent.interrupt_boundaries", "owner/assistant")
	}); err != nil {
		t.Fatal(err)
	}
	page, err = q.List(t.Context(), "owner", canceled)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("stale boundary after removal", err)
	}
	if err := q.Cancel(t.Context(), "owner", "job-000"); err != nil {
		t.Fatal(err)
	}
	job, err := persistence.Load[Job](t.Context(), db, jobsBucket, "owner/job-000")
	if err != nil || job.State != Canceled || job.Version != 2 {
		t.Fatal("Cancel reused stale visible state", job.State, job.Version, err)
	}
}

func TestJobListBoundaryScopeTerminalStatesAndErrors(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "scope.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := []Job{}
	add := func(id, owner, assistant core.ID, state State, kind event.Kind, created, occurrence time.Time) {
		rows = append(rows, Job{State: state, Version: 1, Event: event.AssistantEvent{ID: id, OwnerID: owner, AssistantID: assistant, Kind: kind, CreatedAt: created, OccurrenceAt: occurrence}})
	}
	for _, state := range []State{Queued, Retry, Failed, NeedsReview} {
		add(core.ID("a-"+state), "owner", "a", state, event.UserMessage, now.Add(-time.Minute), time.Time{})
	}
	add("done", "owner", "terminal", Done, event.UserMessage, now.Add(-time.Minute), time.Time{})
	add("canceled", "owner", "terminal", Canceled, event.UserMessage, now.Add(-time.Minute), time.Time{})
	add("stop", "owner", "a", Queued, event.UserStopped, now.Add(-time.Minute), time.Time{})
	add("future", "owner", "a", Queued, event.UserMessage, now.Add(time.Minute), time.Time{})
	add("schedule", "owner", "a", Queued, event.ScheduleDue, now.Add(time.Minute), now.Add(-time.Minute))
	add("b-one", "owner", "b", Queued, event.UserMessage, now.Add(-time.Minute), time.Time{})
	add("b-two", "owner", "b", Queued, event.UserMessage, now.Add(-time.Minute), time.Time{})
	add("foreign", "other", "a", Queued, event.UserMessage, now.Add(-time.Minute), time.Time{})
	if err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, r := range rows {
			if err := tx.Put(ctx, jobsBucket, persistence.Key(r.Event.OwnerID, r.Event.ID), r); err != nil {
				return err
			}
		}
		return tx.Put(ctx, "agent.interrupt_boundaries", "owner/a", event.InterruptBoundary{At: now})
	}); err != nil {
		t.Fatal(err)
	}
	counts := &boundaryReadStore{Store: db}
	q := NewQueue(counts)
	page, err := q.List(t.Context(), "owner", Query{Page: core.PageRequest{Limit: 100}})
	if err != nil || len(page.Items) != 11 || counts.reads != 2 || counts.byKey["owner/a"] != 1 || counts.byKey["owner/b"] != 1 {
		t.Fatalf("scope reads=%v items=%d err=%v", counts.byKey, len(page.Items), err)
	}
	for _, r := range page.Items {
		want := Canceled
		switch r.Event.ID {
		case "done":
			want = Done
		case "stop", "future", "b-one", "b-two":
			want = Queued
		}
		if r.State != want || r.Event.DeliveryMode != r.Event.Delivery() {
			t.Fatalf("wrong visible job %+v", r)
		}
	}
	counts.failKey = "owner/terminal"
	counts.failure = errors.New("synthetic boundary read failure")
	counts.reads = 0
	if _, err := q.List(t.Context(), "owner", Query{AssistantID: "terminal"}); err != nil || counts.reads != 0 {
		t.Fatal("terminal jobs read boundaries", err, counts.reads)
	}
	counts.failKey = "owner/b"
	if _, err := q.List(t.Context(), "owner", Query{}); !errors.Is(err, counts.failure) {
		t.Fatal("boundary error hidden", err)
	}
}
