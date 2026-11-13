package app

import (
	"context"
	"molr.ai/runtime/internal/business/planstate"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"path/filepath"
	"testing"
	"time"
)

func TestPlanDeletionRevokesManagedExecutionAuthorization(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "db"))
	need(t, err)
	defer db.Close()
	e := core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: "assistant", TaskID: "task", OperationID: "shell"}
	ref := core.Object{Kind: "todo", ID: "plan"}
	need(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "business.todos", "owner/plan", map[string]any{"State": "active", "Version": 1}); err != nil {
			return err
		}
		return tx.Put(ctx, "business.tasks", "owner/task", task.Task{ID: "task", OwnerID: "owner", AssistantID: "assistant", Plan: ref, State: task.Completed, Version: 1})
	}))
	authorization, err := executionAuthorization(db, e, func(context.Context) error { return nil })
	need(t, err)
	defer authorization.Watch.Close()
	_, err = authorization.Check(t.Context())
	need(t, err)
	need(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return planstate.Set(ctx, tx, "owner", ref, planstate.Paused, 1)
	}))
	select {
	case <-authorization.Watch.Changes():
	case <-time.After(time.Second):
		t.Fatal("pause notification missing")
	}
	_, err = authorization.Check(t.Context())
	need(t, err)
	need(t, planstate.Delete(t.Context(), db, "owner", ref, 2))
	select {
	case <-authorization.Watch.Changes():
	case <-time.After(time.Second):
		t.Fatal("deletion failed to wake process authorization")
	}
	if _, err = authorization.Check(t.Context()); err == nil {
		t.Fatal("deleted plan authorized managed process")
	}
	if err = checkExecutionPlan(t.Context(), db, e); err == nil {
		t.Fatal("deleted plan allowed a new managed operation")
	}
}

func TestDeletedPlanRejectsLateOperationEvents(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "db"))
	need(t, err)
	defer db.Close()
	ref := core.Object{Kind: "todo", ID: "plan"}
	e := event.AssistantEvent{OwnerID: "owner", AssistantID: "assistant", Kind: event.ShellTaskFinished, OperationID: "shell", ID: "completion"}
	need(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "business.todos", "owner/plan", map[string]any{"State": "active", "Version": 1}); err != nil {
			return err
		}
		if err := tx.Put(ctx, "business.tasks", "owner/task", task.Task{ID: "task", OwnerID: "owner", AssistantID: "assistant", Plan: ref, State: task.Completed, Version: 1}); err != nil {
			return err
		}
		if err := tx.Put(ctx, "agent.operations", "owner/shell", map[string]any{"ID": "shell", "OwnerID": "owner", "TaskID": "task"}); err != nil {
			return err
		}
		return event.SaveJob(ctx, tx, event.Job{Event: e, State: event.Queued, Version: 1})
	}))
	need(t, checkEventPlan(t.Context(), db, e))
	need(t, planstate.Delete(t.Context(), db, "owner", ref, 1))
	job, err := persistence.Load[event.Job](t.Context(), db, "agent.jobs", "owner/completion")
	need(t, err)
	if job.State != event.Canceled {
		t.Fatal("queued operation event survived deletion")
	}
	if err = checkEventPlan(t.Context(), db, e); core.CodeOf(err) != core.Canceled {
		t.Fatal("late operation event started work", err)
	}
}
