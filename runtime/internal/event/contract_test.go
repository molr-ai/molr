package event

import (
	"context"
	"testing"
	"time"

	"molr.ai/runtime/tests/testkit"
)

// Commit/Rollback control the actual transaction to which Writer is bound.
// Events reads committed assistant input records through an independent reader.
type contractFixture struct {
	Writer   Writer
	Commit   func(context.Context) error
	Rollback func(context.Context) error
	Events   func(context.Context) ([]AssistantEvent, error)
}

var newContractFixture func(*testing.T) contractFixture

func TestTDDAssistantWriterDeduplicatesOccurrenceAndCommitsActualSource(t *testing.T) {
	f := testkit.RequireFactory(t, "event", newContractFixture)
	ctx := testkit.Context(t)
	now := time.Date(2026, 9, 23, 8, 0, 0, 0, time.UTC)
	e := AssistantEvent{ID: "e1", OwnerID: "owner", AssistantID: "assistant", Kind: ScheduleDue, ScheduleID: "schedule", OccurrenceAt: now, CreatedAt: now}
	testkit.RequireNoError(t, f.Writer.AppendAssistant(ctx, e))
	e.ID = "retry-transport-id"
	testkit.RequireNoError(t, f.Writer.AppendAssistant(ctx, e))
	e.ID = "next-occurrence"
	e.OccurrenceAt = e.OccurrenceAt.Add(time.Hour)
	testkit.RequireNoError(t, f.Writer.AppendAssistant(ctx, e))
	testkit.RequireNoError(t, f.Commit(ctx))
	events, err := f.Events(ctx)
	testkit.RequireNoError(t, err)
	if len(events) != 2 {
		t.Fatalf("expected two unique occurrences, got %+v", events)
	}
	for _, got := range events {
		if got.Kind != ScheduleDue || got.MessageID != "" || got.ScheduleID != "schedule" {
			t.Fatalf("schedule became chat input: %+v", got)
		}
	}
}
func TestTDDAssistantWriterRollbackAndInvalidSource(t *testing.T) {
	f := testkit.RequireFactory(t, "event", newContractFixture)
	ctx := testkit.Context(t)
	e := AssistantEvent{ID: "e1", OwnerID: "owner", AssistantID: "assistant", Kind: UserMessage, MessageID: "message", CreatedAt: time.Now()}
	testkit.RequireNoError(t, f.Writer.AppendAssistant(ctx, e))
	testkit.RequireNoError(t, f.Rollback(ctx))
	events, err := f.Events(ctx)
	testkit.RequireNoError(t, err)
	if len(events) != 0 {
		t.Fatalf("rolled back event persisted: %+v", events)
	}
}
func TestTDDAssistantWriterRejectsInvalidSource(t *testing.T) {
	f := testkit.RequireFactory(t, "event", newContractFixture)
	ctx := testkit.Context(t)
	e := AssistantEvent{ID: "bad", OwnerID: "owner", AssistantID: "assistant", Kind: ScheduleDue, MessageID: "fake-user", CreatedAt: time.Now()}
	if err := f.Writer.AppendAssistant(ctx, e); err == nil {
		t.Fatal("invalid scheduled event accepted")
	}
	testkit.RequireNoError(t, f.Commit(ctx))
	events, err := f.Events(ctx)
	testkit.RequireNoError(t, err)
	if len(events) != 0 {
		t.Fatal("invalid event persisted")
	}
}
