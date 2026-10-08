package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
)

func TestDreamCommitPlanSurvivesEngineFailure(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	f.Turn.Event.Kind = event.DreamDue
	f.Turn.Event.MessageID = ""
	f.Turn.Event.OperationID = "dream-run"
	f.Turn.Event.OccurrenceAt = time.Now()
	e.options.DreamDeadline = func(context.Context, core.Principal, event.AssistantEvent) (time.Time, error) {
		return time.Now().Add(time.Minute), nil
	}
	e.options.DreamPrepare = func(context.Context, core.Principal, event.AssistantEvent) (string, error) {
		return `{ "Sources": [] }`, nil
	}
	target := filepath.Join(t.TempDir(), "memory.md")
	expected := errors.New("failure after file commit")
	e.options.DreamCommit = func(ctx context.Context, _ core.Principal, _ event.AssistantEvent, _ string) error {
		if err := db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, "test.dream_plan", "plan", "durable")
		}); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte("committed cognition"), 0600); err != nil {
			return err
		}
		return expected
	}
	if _, err := e.Handle(t.Context(), f.Turn); !errors.Is(err, expected) {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal(err)
	}
	plan, err := persistence.Load[string](t.Context(), db, "test.dream_plan", "plan")
	if err != nil || plan != "durable" {
		t.Fatal("engine rolled back recovery plan after a filesystem effect", plan, err)
	}
}

func TestDreamRecoveryUsesSavedOutputWithoutModelCall(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	f.Turn.Event.Kind = event.DreamDue
	f.Turn.Event.MessageID = ""
	f.Turn.Event.OperationID = "dream-run"
	f.Turn.Event.OccurrenceAt = time.Now()
	saved := `{"Summary":"durable result","Memories":[]}`
	e.options.DreamDeadline = func(context.Context, core.Principal, event.AssistantEvent) (time.Time, error) {
		return time.Time{}, nil
	}
	e.options.DreamPrepare = func(context.Context, core.Principal, event.AssistantEvent) (string, error) { return `{}`, nil }
	e.options.DreamOutput = func(context.Context, core.Principal, event.AssistantEvent) (string, error) { return saved, nil }
	committed := false
	e.options.DreamCommit = func(_ context.Context, _ core.Principal, _ event.AssistantEvent, output string) error {
		if output != saved {
			t.Fatal(output)
		}
		committed = true
		return nil
	}
	if _, err := e.Handle(t.Context(), f.Turn); err != nil {
		t.Fatal(err)
	}
	if !committed || len(f.ModelRequests()) != 0 {
		t.Fatal("recovery generated another paid model request")
	}
}
