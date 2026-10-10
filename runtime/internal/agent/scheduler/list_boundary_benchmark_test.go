package scheduler

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkJobListSharedBoundary(b *testing.B) {
	db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "jobs.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	err = db.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "agent.interrupt_boundaries", "owner/assistant", event.InterruptBoundary{At: now}); err != nil {
			return err
		}
		for i := 0; i < 500; i++ {
			id := core.ID(fmt.Sprintf("job-%03d", i))
			job := Job{State: Queued, Version: 1, Event: event.AssistantEvent{ID: id, OwnerID: "owner", AssistantID: "assistant", Kind: event.UserMessage, CreatedAt: now.Add(-time.Minute)}}
			if err := tx.Put(ctx, jobsBucket, persistence.Key("owner", id), job); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	q := NewQueue(db)
	request := Query{AssistantID: "assistant", States: []State{Canceled}, Page: core.PageRequest{Limit: 200}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		page, err := q.List(b.Context(), "owner", request)
		if err != nil || len(page.Items) != 200 {
			b.Fatalf("page=%d err=%v", len(page.Items), err)
		}
	}
}
