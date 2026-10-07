package event

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
)

func TestTDDAssistantWriterBindsSourceAndExpiresWithTransaction(t *testing.T) {
	ctx := testkit.Context(t)
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "event.db"))
	testkit.RequireNoError(t, err)
	defer db.Close()
	writer := NewWriter(db)
	e := AssistantEvent{ID: "event", OwnerID: "owner", AssistantID: "assistant", Kind: UserMessage, MessageID: "message", CreatedAt: time.Now()}
	testkit.RequireNoError(t, writer.AppendAssistant(ctx, e))
	changed := e
	changed.MessageID = "different-message"
	testkit.RequireCode(t, writer.AppendAssistant(ctx, changed), core.Conflict)
	var expired Writer
	testkit.RequireNoError(t, db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { expired = NewWriter(tx.Store()); return nil }))
	e.ID = "late"
	e.MessageID = "late-message"
	testkit.RequireCode(t, expired.AppendAssistant(ctx, e), core.FailedPrecondition)
	events, err := persistence.Scan[AssistantEvent](ctx, db, eventsBucket)
	testkit.RequireNoError(t, err)
	if len(events) != 1 {
		t.Fatal("conflicting or expired transaction persisted an event")
	}
}

func TestTDDEventTaskAndScheduleRevisionIdentity(t *testing.T) {
	ctx := testkit.Context(t)
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sources.db"))
	testkit.RequireNoError(t, err)
	defer db.Close()
	writer := NewWriter(db)
	now := time.Now()
	taskEvent := AssistantEvent{ID: "task-event", OwnerID: "owner", AssistantID: "assistant", Kind: ResumeRequested, TaskID: "task", CreatedAt: now}
	testkit.RequireNoError(t, taskEvent.Validate())
	testkit.RequireNoError(t, writer.AppendAssistant(ctx, taskEvent))
	invalid := taskEvent
	invalid.Kind = UserMessage
	invalid.MessageID = "message"
	testkit.RequireCode(t, invalid.Validate(), core.InvalidArgument)
	invalid = taskEvent
	invalid.TaskID = "../task"
	testkit.RequireCode(t, invalid.Validate(), core.InvalidArgument)
	first := AssistantEvent{ID: "schedule-v1", OwnerID: "owner", AssistantID: "assistant", Kind: ScheduleDue, ScheduleID: "schedule", ScheduleVersion: 1, OccurrenceAt: now, CreatedAt: now}
	testkit.RequireNoError(t, writer.AppendAssistant(ctx, first))
	second := first
	second.ID = "schedule-v2"
	second.ScheduleVersion = 2
	testkit.RequireNoError(t, writer.AppendAssistant(ctx, second))
	duplicate := second
	duplicate.ID = "schedule-retry"
	testkit.RequireNoError(t, writer.AppendAssistant(ctx, duplicate))
	rows, err := persistence.Scan[AssistantEvent](ctx, db, eventsBucket)
	testkit.RequireNoError(t, err)
	if len(rows) != 3 {
		t.Fatalf("task and versioned occurrences = %+v", rows)
	}
}
