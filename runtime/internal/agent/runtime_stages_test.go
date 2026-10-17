package agent

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/debugtrace"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/tests/testkit"
)

func TestTurnCleanupFailurePreservesOriginalErrorAndReportsDiagnostic(t *testing.T) {
	for _, original := range []error{
		core.NewError(core.BudgetExceeded, "budget exhausted"),
		modelTerminal(core.WithReason(core.NewError(core.FailedPrecondition, "binding changed"), core.ModelBindingChanged)),
		context.Canceled,
		context.DeadlineExceeded,
	} {
		t.Run(string(core.CodeOf(original)), func(t *testing.T) {
			f, db := makeLoopFixture(t, nil)
			e := f.Runtime.(*Engine)
			trace := debugtrace.New(db, debugtrace.Options{Enabled: true, Authorize: func(context.Context, core.Principal) error { return nil }})
			testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
				return tx.Put(ctx, "business.settings", string(f.Owner.OwnerID), map[string]bool{"WorkbenchVisible": true})
			}))
			e.options.Trace = trace
			e.options.ResolveModel = func(context.Context, core.Principal, core.ID) (core.ID, error) {
				// The first remaining write is failure status cleanup. A cleanup deadline
				// must never replace the original budget/model classification.
				db.FailNextCommit(context.DeadlineExceeded)
				return "", original
			}
			var logs bytes.Buffer
			previous := log.Writer()
			log.SetOutput(&logs)
			defer log.SetOutput(previous)
			_, err := e.Handle(t.Context(), f.Turn)
			if err != original || core.CodeOf(err) != core.CodeOf(original) || core.ReasonOf(err) != core.ReasonOf(original) {
				t.Fatalf("cleanup changed original error: %v", err)
			}
			if !strings.Contains(logs.String(), "agent turn status cleanup failed") {
				t.Fatal("cleanup failure was not logged")
			}
			status, err := e.Status(t.Context(), f.Owner, f.Turn.Event.AssistantID)
			testkit.RequireNoError(t, err)
			if status.State != Working {
				t.Fatalf("failed status transaction escaped rollback: %+v", status)
			}
			timing, err := persistence.Load[turnTiming](t.Context(), db, "agent.turn_timing", "owner/run")
			testkit.RequireNoError(t, err)
			if timing.EndedAt == nil {
				t.Fatal("status cleanup failure prevented timing cleanup")
			}
			runs, err := trace.Runs(t.Context(), f.Owner, debugtrace.Query{})
			testkit.RequireNoError(t, err)
			if len(runs.Items) != 1 {
				t.Fatalf("trace runs: %d", len(runs.Items))
			}
			run, err := trace.Get(t.Context(), f.Owner, runs.Items[0].ID)
			testkit.RequireNoError(t, err)
			found := false
			for _, step := range run.Steps {
				if step.Kind == "turn.cleanup_failed" {
					found = true
				}
			}
			if !found {
				t.Fatal("cleanup failure missing from turn trace")
			}
		})
	}
}

func TestTurnCleanupKeepsDeferredAndRuntimeInterruptedState(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		t.Run(map[bool]string{false: "deferred", true: "runtime-interrupted"}[interrupted], func(t *testing.T) {
			f, db := makeLoopFixture(t, nil)
			e := f.Runtime.(*Engine)
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			var original error = &scheduler.Deferred{At: time.Now().Add(time.Second), Cause: core.NewError(core.Unavailable, "retry later")}
			if interrupted {
				original = context.Canceled
			}
			e.options.ResolveModel = func(context.Context, core.Principal, core.ID) (core.ID, error) {
				if interrupted {
					cancel(scheduler.ErrRuntimeInterrupted)
				}
				return "", original
			}
			_, err := e.Handle(ctx, f.Turn)
			if err != original {
				t.Fatalf("changed retry/interruption error: %v", err)
			}
			status, err := e.Status(t.Context(), f.Owner, f.Turn.Event.AssistantID)
			testkit.RequireNoError(t, err)
			if status.State != Working || status.WaitingReason != "" {
				t.Fatalf("recovery state was overwritten: %+v", status)
			}
			timing, err := persistence.Load[turnTiming](t.Context(), db, "agent.turn_timing", "owner/run")
			testkit.RequireNoError(t, err)
			if (timing.EndedAt != nil) != interrupted {
				t.Fatalf("incorrect attempt timing: %+v", timing)
			}
		})
	}
}

func TestTurnCommitFailureRetriesWithoutReplayingPublishedTool(t *testing.T) {
	for _, failure := range []error{errors.New("acknowledgement commit failed"), context.Canceled} {
		t.Run(failure.Error(), func(t *testing.T) {
			var calls atomic.Int32
			f, db := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					completeModel(w, []any{stateTestCall("publish", "conversation_publish", `{"conversation_id":"chat","text":"one publication"}`)})
				} else {
					completeModel(w, nil)
				}
			}))
			e := f.Runtime.(*Engine)
			failed := false
			e.options.Acknowledge = func(context.Context, Turn) error {
				if !failed {
					failed = true
					return failure
				}
				return nil
			}
			_, err := e.Handle(t.Context(), f.Turn)
			if err != failure {
				t.Fatalf("expected final transaction failure, got %v", err)
			}
			handledKey := persistence.Key(f.Owner.OwnerID, stableID(string(f.Turn.Event.AssistantID), string(f.Turn.Event.ID)))
			if _, err := persistence.Load[Outcome](t.Context(), db, "agent.handled", handledKey); !persistence.Missing(err) {
				t.Fatalf("failed commit left receipt: %v", err)
			}
			cp, err := f.Checkpoints.Load(t.Context(), f.Owner.OwnerID, f.Turn.Event.AssistantID)
			testkit.RequireNoError(t, err)
			saved, err := decodeState(cp)
			testkit.RequireNoError(t, err)
			if saved.Current == nil {
				t.Fatal("failed commit lost resumable cursor")
			}
			out, err := e.Handle(t.Context(), f.Turn)
			testkit.RequireNoError(t, err)
			if out.Status.State != Idle || out.Checkpoint.LastEventID != f.Turn.Event.ID {
				t.Fatalf("retry did not finish: %+v", out.Status)
			}
			if calls.Load() != 2 || f.VisibleMessageCount() != 1 {
				t.Fatalf("retry repeated model/tool: models=%d publications=%d", calls.Load(), f.VisibleMessageCount())
			}
			_, err = e.Handle(t.Context(), f.Turn)
			testkit.RequireNoError(t, err)
			if calls.Load() != 2 || f.VisibleMessageCount() != 1 {
				t.Fatal("completed retry was not idempotent")
			}
		})
	}
}

func TestDreamDeadlineCleanupPreservesIdleState(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	f.Turn.Event.Kind = event.DreamDue
	f.Turn.Event.MessageID = ""
	f.Turn.Event.OperationID = "dream-run"
	f.Turn.Event.OccurrenceAt = time.Now()
	e.options.DreamDeadline = func(context.Context, core.Principal, event.AssistantEvent) (time.Time, error) {
		return time.Now().Add(-time.Second), nil
	}
	_, err := e.Handle(t.Context(), f.Turn)
	testkit.RequireCode(t, err, core.DeadlineExceeded)
	status, err := e.Status(t.Context(), f.Owner, f.Turn.Event.AssistantID)
	testkit.RequireNoError(t, err)
	if status.State != Idle || status.DreamDeadline != nil || status.DreamRunID != "" {
		t.Fatalf("expired dream remained active: %+v", status)
	}
}
