package app

import (
	"context"
	"errors"
	"fmt"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
	"path/filepath"
	"testing"
)

func TestExecutionChecksReadOnlyMatchingJobs(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "access.db"))
	testkit.RequireNoError(t, err)
	defer db.Close()
	p := core.Principal{Kind: core.ActorAssistant, OwnerID: "owner", SubjectID: "assistant", ExecutionID: "execution"}
	testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "business.assistants", "owner/assistant", assistant.Assistant{ID: "assistant", OwnerID: "owner", State: assistant.Active}); err != nil {
			return err
		}
		for i := 0; i < 1000; i++ {
			id := core.ID(fmt.Sprintf("history-%04d", i))
			if err := event.SaveJob(ctx, tx, scheduler.Job{Event: event.AssistantEvent{OwnerID: p.OwnerID, AssistantID: p.SubjectID, ID: id}, ExecutionID: id, State: scheduler.Done}); err != nil {
				return err
			}
		}
		return event.SaveJob(ctx, tx, scheduler.Job{Event: event.AssistantEvent{OwnerID: p.OwnerID, AssistantID: p.SubjectID, ID: "current"}, ExecutionID: p.ExecutionID, State: scheduler.Running})
	}))
	observed := &testkit.ObservedStore{Store: db}
	a := access{observed}
	testkit.RequireNoError(t, a.liveAssistant(t.Context(), p))
	execution := core.Execution{Principal: p, AssistantID: p.SubjectID, OperationID: "operation"}
	testkit.RequireNoError(t, a.historicalExecution(t.Context(), execution))
	execution.TaskID = "foreign-task"
	testkit.RequireCode(t, a.historicalExecution(t.Context(), execution), core.Forbidden)
	p.ExecutionID = "missing"
	testkit.RequireCode(t, a.liveAssistant(t.Context(), p), core.Forbidden)
	if observed.Scans != 0 || observed.Rows["agent.jobs"] != 2 {
		t.Fatalf("unrelated jobs loaded: %+v", observed)
	}
}

func TestExecutionDomainChecksPreserveLifecycleAndTransaction(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "access.db"))
	testkit.RequireNoError(t, err)
	defer db.Close()
	p := core.Principal{Kind: core.ActorAssistant, OwnerID: "owner", SubjectID: "assistant", ExecutionID: "execution"}
	e := core.Execution{Principal: p, AssistantID: p.SubjectID, OperationID: "operation", TaskID: "task"}
	a := access{db}
	save := func(ctx context.Context, tx persistence.Tx, state scheduler.State, taskState task.State, assistantState assistant.State) error {
		if err := tx.Put(ctx, "business.assistants", "owner/assistant", assistant.Assistant{ID: "assistant", OwnerID: "owner", State: assistantState}); err != nil {
			return err
		}
		if err := tx.Put(ctx, "business.tasks", "owner/task", task.Task{ID: "task", OwnerID: "owner", AssistantID: "assistant", State: taskState}); err != nil {
			return err
		}
		return event.SaveJob(ctx, tx, scheduler.Job{Event: event.AssistantEvent{ID: "job", OwnerID: "owner", AssistantID: "assistant", TaskID: "task"}, ExecutionID: "execution", State: state})
	}
	testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return save(ctx, tx, scheduler.Running, task.Running, assistant.Active)
	}))
	testkit.RequireNoError(t, a.execution(t.Context(), e))
	for _, state := range []task.State{task.Pending, task.Running, task.Waiting, task.Paused, task.Completed, task.Failed, task.Canceled} {
		t.Run(string(state), func(t *testing.T) {
			testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
				return save(ctx, tx, scheduler.Running, state, assistant.Active)
			}))
			err := a.execution(t.Context(), e)
			if state == task.Pending || state == task.Running || state == task.Waiting {
				testkit.RequireNoError(t, err)
			} else {
				testkit.RequireCode(t, err, core.Forbidden)
			}
		})
	}
	testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return save(ctx, tx, scheduler.Done, task.Completed, assistant.Dismissed)
	}))
	testkit.RequireNoError(t, a.historicalExecution(t.Context(), e))
	testkit.RequireCode(t, scheduler.RequireRunningExecution(t.Context(), db, p), core.Forbidden)
	_, err = assistant.ResolveActive(t.Context(), db, p, p.SubjectID)
	testkit.RequireCode(t, err, core.Forbidden)
	rollback := errors.New("rollback")
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := save(ctx, tx, scheduler.Running, task.Running, assistant.Active); err != nil {
			return err
		}
		testkit.RequireNoError(t, a.execution(ctx, e))
		other := p
		other.OwnerID = "other"
		testkit.RequireCode(t, scheduler.RequireRunningExecution(ctx, db, other), core.Forbidden)
		forged := e
		forged.TaskID = "other-task"
		testkit.RequireCode(t, scheduler.RequireHistoricalExecution(ctx, db, forged), core.Forbidden)
		testkit.RequireCode(t, task.RequireExecution(ctx, db, p.OwnerID, e.TaskID, "other-assistant"), core.Forbidden)
		_, err := assistant.ResolveActive(ctx, db, p, "other-assistant")
		testkit.RequireCode(t, err, core.Forbidden)
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	testkit.RequireCode(t, scheduler.RequireRunningExecution(t.Context(), db, p), core.Forbidden)
	testkit.RequireNoError(t, a.historicalExecution(t.Context(), e))
}
