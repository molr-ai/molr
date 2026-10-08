package app

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/business/assistant"
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
