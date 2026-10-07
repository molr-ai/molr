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
