package scheduler

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
	"path/filepath"
	"testing"
	"time"
)

func TestPendingDeliveryHonorsClaimAndStopBoundaries(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "delivery.db"))
	testkit.RequireNoError(t, err)
	defer db.Close()
	now := time.Now()
	for _, mode := range []event.DeliveryMode{event.Interrupt, event.AfterStep, event.WhenIdle} {
		testkit.RequireNoError(t, event.NewWriter(db).AppendAssistant(t.Context(), event.AssistantEvent{ID: core.ID(mode), OwnerID: "owner", AssistantID: "assistant", Kind: event.ShellTaskFinished, OperationID: core.ID(mode), DeliveryMode: mode, CreatedAt: now}))
	}
	jobs, err := Pending(t.Context(), db, "owner", "assistant", "active", now, false)
	testkit.RequireNoError(t, err)
	if len(jobs) != 2 {
		t.Fatal(jobs)
	}
	urgent, err := Pending(t.Context(), db, "owner", "assistant", "active", now, true)
	testkit.RequireNoError(t, err)
	if len(urgent) != 1 || urgent[0].Event.Delivery() != event.Interrupt {
		t.Fatal(urgent)
	}
	testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "agent.interrupt_boundaries", "owner/assistant", event.InterruptBoundary{At: now})
	}))
	jobs, err = Pending(t.Context(), db, "owner", "assistant", "active", now, false)
	testkit.RequireNoError(t, err)
	if len(jobs) != 0 {
		t.Fatal("stop resurrected events", jobs)
	}
}

func TestPendingDeadlineExcludesIneligibleInputs(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "deadline.db"))
	testkit.RequireNoError(t, err)
	defer db.Close()
	now := time.Now()
	due := now.Add(time.Hour)
	testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, id := range []core.ID{"due", "joined", "running", "idle", "after-step", "blocked", "active", "foreign-owner", "foreign-assistant"} {
			job := event.Job{Event: event.AssistantEvent{ID: id, OwnerID: "owner", AssistantID: "assistant", Kind: event.ShellTaskFinished, OperationID: id, DeliveryMode: event.Interrupt, CreatedAt: now}, State: event.Queued, NotBefore: now.Add(time.Minute), Version: 1}
			switch id {
			case "due":
				job.NotBefore = due
			case "joined":
				job.JoinedTo = "turn"
			case "running":
				job.State = event.Running
			case "idle":
				job.Event.DeliveryMode = event.WhenIdle
			case "after-step":
				job.Event.DeliveryMode = event.AfterStep
			case "blocked":
				job.Event.CreatedAt = now.Add(-time.Hour)
			case "foreign-owner":
				job.Event.OwnerID = "other"
			case "foreign-assistant":
				job.Event.AssistantID = "other"
			}
			if err := event.SaveJob(ctx, tx, job); err != nil {
				return err
			}
		}
		return tx.Put(ctx, "agent.interrupt_boundaries", "owner/assistant", event.InterruptBoundary{At: now.Add(-time.Minute)})
	}))
	jobs, next, err := PendingWithDeadline(t.Context(), db, "owner", "assistant", "active", now, true)
	testkit.RequireNoError(t, err)
	if len(jobs) != 0 || !next.Equal(due) {
		t.Fatal(jobs, next)
	}
	jobs, next, err = PendingWithDeadline(t.Context(), db, "owner", "assistant", "active", due, true)
	testkit.RequireNoError(t, err)
	if len(jobs) != 1 || jobs[0].Event.ID != "due" || !next.IsZero() {
		t.Fatal(jobs, next)
	}
}
