package scheduler

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
)

func TestJobListPendingExcludesHistory(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "pending.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < 1004; i++ {
			state, assistant, owner := Done, core.ID("assistant"), core.ID("owner")
			switch i {
			case 1000:
				state = Queued
			case 1001:
				state = Retry
			case 1002:
				state, assistant = Queued, "other-assistant"
			case 1003:
				state, owner = Queued, "other-owner"
			}
			id := core.ID(fmt.Sprintf("job-%04d", i))
			job := Job{State: state, Version: 1, Event: event.AssistantEvent{ID: id, OwnerID: owner, AssistantID: assistant, Kind: event.UserMessage}}
			if err := tx.Put(ctx, jobsBucket, persistence.Key(owner, id), job); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, assistant := range []core.ID{"", "assistant"} {
		observed := &testkit.ObservedStore{Store: db}
		page, err := NewQueue(observed).List(t.Context(), "owner", Query{AssistantID: assistant, States: []State{Queued, Retry}, Page: core.PageRequest{Limit: 200}})
		want := 2
		if assistant == "" {
			want = 3
		}
		if err != nil || len(page.Items) != want || page.NextCursor != "" {
			t.Fatalf("pending page: %+v %v", page, err)
		}
		if observed.Scans != 0 || observed.Rows[jobsBucket] != want {
			t.Fatalf("history loaded: scans=%d rows=%d", observed.Scans, observed.Rows[jobsBucket])
		}
		for _, job := range page.Items {
			if job.Event.OwnerID != "owner" || !slices.Contains([]State{Queued, Retry}, job.State) {
				t.Fatalf("unrelated job: %+v", job)
			}
		}
	}
}

func TestJobListStateFiltersPreserveVisiblePages(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "states.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, assistant := range []core.ID{"stopped", "active"} {
			for i, state := range []State{Queued, Retry, Running, Failed, NeedsReview, Done, Canceled, Interrupted, Queued, Retry} {
				id := core.ID(fmt.Sprintf("job-%02d-%s", i, assistant))
				job := Job{State: state, Version: 1, Event: event.AssistantEvent{ID: id, OwnerID: "owner", AssistantID: assistant, Kind: event.UserMessage, MessageSequence: uint64(i + 1), CreatedAt: now.Add(-time.Minute)}}
				if err := tx.Put(ctx, jobsBucket, persistence.Key("owner", id), job); err != nil {
					return err
				}
			}
		}
		return tx.Put(ctx, "agent.interrupt_boundaries", "owner/stopped", event.InterruptBoundary{At: now, ConversationID: "chat", ThroughSequence: 5})
	})
	if err != nil {
		t.Fatal(err)
	}
	q := NewQueue(db)
	for _, assistant := range []core.ID{"", "stopped", "active"} {
		all, err := q.List(t.Context(), "owner", Query{AssistantID: assistant})
		if err != nil {
			t.Fatal(err)
		}
		for _, states := range [][]State{{Queued, Retry}, {Done}, {Failed, NeedsReview}, {Canceled}, {Canceled, Queued}, {Running, Retry}, {Interrupted}, {"unknown"}, {Queued, Queued, Retry}} {
			want := []Job{}
			for _, job := range all.Items {
				if slices.Contains(states, job.State) {
					want = append(want, job)
				}
			}
			got := []Job{}
			query := Query{AssistantID: assistant, States: states, Page: core.PageRequest{Limit: 2}}
			for pages := 0; ; pages++ {
				if pages > 20 {
					t.Fatal("pagination did not finish")
				}
				page, err := q.List(t.Context(), "owner", query)
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, page.Items...)
				if page.NextCursor == "" {
					break
				}
				query.Page.Cursor = page.NextCursor
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("assistant=%s states=%v\ngot=%+v\nwant=%+v", assistant, states, got, want)
			}
		}
	}
}
