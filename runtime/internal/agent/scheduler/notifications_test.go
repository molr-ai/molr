package scheduler

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func TestFailureNoticesDistinguishDreamsAndResolveOnCompletion(t *testing.T) {
	for _, kind := range []event.Kind{event.DreamDue, event.ScheduleDue} {
		for _, state := range []State{Failed, NeedsReview} {
			t.Run(string(kind)+"/"+string(state), func(t *testing.T) {
				db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "notices.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				job := Job{Event: event.AssistantEvent{ID: "event", OwnerID: "owner", AssistantID: "assistant", Kind: kind, OperationID: "batch"}, State: state, FailureCode: core.DeadlineExceeded, NotBefore: time.Now(), Version: 1}
				save := func() {
					t.Helper()
					if err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return saveJob(ctx, tx, job) }); err != nil {
						t.Fatal(err)
					}
				}
				save()
				save()
				rows, err := persistence.Scan[notification.Notice](t.Context(), db, "notifications.records")
				if err != nil || len(rows) != 1 {
					t.Fatal(rows, err)
				}
				want := "execution.failed"
				if kind == event.DreamDue {
					want = "dream.failed"
				}
				n := rows[0]
				if n.Kind != want || n.Code != string(state)+":deadline_exceeded" || n.AssistantID != "assistant" || n.Source.ID != "event" || n.ResolvedAt != nil {
					t.Fatal(n)
				}
				job.State = Done
				job.FailureCode = ""
				job.Version++
				save()
				rows, err = persistence.Scan[notification.Notice](t.Context(), db, "notifications.records")
				if err != nil || len(rows) != 1 || rows[0].ResolvedAt == nil {
					t.Fatal("completion left active notice", rows, err)
				}
			})
		}
	}
}
