package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/tests/testkit"
)

func seedInterrupted(t *testing.T, e *Engine, turn Turn) scheduler.Job {
	t.Helper()
	job := scheduler.Job{Event: turn.Event, State: scheduler.Running, ExecutionID: turn.Principal.ExecutionID, Version: 1, NotBefore: turn.Event.CreatedAt}
	testkit.RequireNoError(t, e.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := event.SaveJob(ctx, tx, job); err != nil {
			return err
		}
		return e.saveStatus(ctx, tx, persistence.Key(turn.Principal.OwnerID, turn.Event.AssistantID), Status{AssistantID: turn.Event.AssistantID, State: Working, ExecutionID: turn.Principal.ExecutionID})
	}))
	return job
}
func recoveryClaim(t *testing.T, e *Engine, job scheduler.Job) scheduler.Claim {
	t.Helper()
	worker := scheduler.NewService(e.store, scheduler.Options{RecoverInterrupted: e.RecoverInterrupted})
	testkit.RequireNoError(t, worker.Recover(t.Context()))
	testkit.RequireNoError(t, worker.Recover(t.Context())) // No duplicate resume on repeated startup.
	next, err := scheduler.NewQueue(e.store).Claim(t.Context(), time.Now().Add(time.Second))
	testkit.RequireNoError(t, err)
	if next.Job.Event.Recovery == nil || next.Job.Event.Recovery.PreviousEventID != job.Event.ID || next.Principal.ExecutionID == job.ExecutionID {
		t.Fatalf("not an independent resume: %+v", next)
	}
	return next
}
func TestRestartCreatesResumeWithCommittedAndUnstartedToolEvidence(t *testing.T) {
	var calls atomic.Int32
	bodies := make(chan string, 4)
	f, _ := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies <- string(b)
		if calls.Add(1) == 1 {
			completeModel(w, []any{stateTestCall("done", "conversation_publish", `{"conversation_id":"chat","text":"published once"}`), stateTestCall("pending", "conversation_publish", `{"conversation_id":"chat","text":"do not replay"}`)})
		} else {
			completeModel(w, nil)
		}
	}))
	e := f.Runtime.(*Engine)
	ctx, cancel := context.WithCancelCause(t.Context())
	e.options.PrepareToolResult = func(_ context.Context, _ core.Execution, r tools.Result) (tools.Result, error) {
		cancel(scheduler.ErrRuntimeInterrupted)
		return r, context.Canceled
	}
	job := seedInterrupted(t, e, f.Turn)
	_, err := e.Handle(ctx, f.Turn)
	if err == nil {
		t.Fatal("expected interrupted turn")
	}
	status, err := e.Status(t.Context(), f.Owner, "assistant")
	testkit.RequireNoError(t, err)
	if status.State != Working || status.WaitingReason != "" {
		t.Fatalf("shutdown created failure: %+v", status)
	}
	restarted := f.Restart().(*Engine)
	claim := recoveryClaim(t, restarted, job)
	_, err = restarted.Handle(t.Context(), Turn{Principal: claim.Principal, Event: claim.Job.Event})
	testkit.RequireNoError(t, err)
	<-bodies
	body := <-bodies
	for _, fragment := range []string{"Runtime recovery:", "runtime_interrupted", "published once", "not_started", "PreviousExecutionID", "conversation_read", "unknown result alone"} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("missing %q in recovery request", fragment)
		}
	}
	if f.VisibleMessageCount() != 1 || calls.Load() != 2 {
		t.Fatal("recovery replayed the old tool queue")
	}
	old, err := persistence.Load[scheduler.Job](t.Context(), e.store, "agent.jobs", "owner/event")
	testkit.RequireNoError(t, err)
	if old.State != scheduler.Interrupted || old.RecoveryEventID != claim.Job.Event.ID {
		t.Fatalf("old execution not fenced: %+v", old)
	}
	_, err = e.Handle(t.Context(), f.Turn)
	testkit.RequireCode(t, err, core.Canceled)
}

func TestRestartUnknownToolOutcomeDoesNotBlockModelRecovery(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	job := seedInterrupted(t, e, f.Turn)
	call := model.ToolCall{ID: "uncertain", Name: "file_write", Arguments: json.RawMessage(`{"path":"result.txt","text":"value"}`)}
	response := model.Input{Role: model.Assistant, Calls: []model.ToolCall{call}}
	p := turnProgress{Step: 7, Inputs: []model.Input{{Role: model.System, Text: loopInstructions}, response}, Response: &response}
	testkit.RequireNoError(t, e.saveProgress(t.Context(), f.Turn, &p, progressAlways))
	id := pendingCall(*cursor(f.Turn, p), call).OperationID
	testkit.RequireNoError(t, e.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "agent.operations", persistence.Key("owner", id), operation.Operation{ID: id, OwnerID: "owner", AssistantID: "assistant", ExecutionID: "run", Action: call.Name, State: operation.Unknown, Version: 1})
	}))
	claim := recoveryClaim(t, e, job)
	_, err := e.Handle(t.Context(), Turn{Principal: claim.Principal, Event: claim.Job.Event})
	testkit.RequireNoError(t, err)
	req := f.ModelRequests()
	if len(req) != 1 {
		t.Fatal("unknown outcome blocked recovery", len(req))
	}
	raw, _ := json.Marshal(req[0].Inputs)
	for _, s := range []string{"side_effects_possible", "unknown", string(id)} {
		if !strings.Contains(string(raw), s) {
			t.Fatal("missing evidence", s)
		}
	}
	if claim.Job.Event.Recovery.Step != 7 {
		t.Fatal("step budget reset")
	}
}

func TestRestartStopAndNewerEventPreventRecovery(t *testing.T) {
	for _, mode := range []string{"stop-before", "stop-after", "reset", "newer"} {
		t.Run(mode, func(t *testing.T) {
			f, _ := makeLoopFixture(t, nil)
			e := f.Runtime.(*Engine)
			job := seedInterrupted(t, e, f.Turn)
			p := turnProgress{Inputs: []model.Input{{Role: model.System, Text: loopInstructions}, {Role: model.User, Text: "original work"}}}
			testkit.RequireNoError(t, e.saveProgress(t.Context(), f.Turn, &p, progressAlways))
			if mode == "stop-before" {
				testkit.RequireNoError(t, e.Stop(t.Context(), f.Owner, "assistant"))
			}
			_, err := e.RecoverInterrupted(t.Context(), job)
			testkit.RequireNoError(t, err)
			if mode == "stop-after" {
				testkit.RequireNoError(t, e.Stop(t.Context(), f.Owner, "assistant"))
			}
			if mode == "reset" {
				testkit.RequireNoError(t, e.ResetContext(t.Context(), f.Owner, "assistant", core.Mutation{IdempotencyKey: "reset-recovery"}, p.Revision))
			}
			if mode == "newer" {
				_, err = e.Handle(t.Context(), nextStateTurn(f, "new-user-turn", event.ResumeRequested))
				testkit.RequireNoError(t, err)
			}
			_, err = scheduler.NewQueue(e.store).Claim(t.Context(), time.Now().Add(time.Second))
			if err == nil {
				t.Fatal("stale recovery was claimable")
			}
		})
	}
}

func TestRestartRecoveryTransactionRollsBackAndDeduplicates(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	job := seedInterrupted(t, e, f.Turn)
	db.FailNextCommit(errors.New("restart recovery commit failed"))
	_, err := e.RecoverInterrupted(t.Context(), job)
	if err == nil {
		t.Fatal("expected rollback")
	}
	prior, err := persistence.Load[scheduler.Job](t.Context(), db, "agent.jobs", "owner/event")
	testkit.RequireNoError(t, err)
	if prior.State != scheduler.Running || prior.RecoveryEventID != "" {
		t.Fatal("partial handoff committed")
	}
	recoveryClaim(t, e, job)
	rows, err := persistence.Select[scheduler.Job](t.Context(), db, "agent.jobs", persistence.Query{})
	testkit.RequireNoError(t, err)
	if len(rows) != 2 {
		t.Fatal("duplicate resume events", len(rows))
	}
}

func TestRestartRecoveryLimitAndProgress(t *testing.T) {
	for _, progress := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-progress", true: "progress"}[progress], func(t *testing.T) {
			f, _ := makeLoopFixture(t, nil)
			e := f.Runtime.(*Engine)
			turn := nextStateTurn(f, "third-resume", event.ResumeRequested)
			turn.Event.Recovery = &event.Recovery{PreviousEventID: "prior", PreviousExecutionID: "prior-run", SourceKind: event.UserMessage, SourceCreatedAt: f.Turn.Event.CreatedAt, Reason: "runtime_interrupted", Attempts: 3, Step: 4}
			p := turnProgress{Step: 4, Inputs: []model.Input{{Role: model.System, Text: loopInstructions}}}
			if progress {
				p.Step++
			}
			testkit.RequireNoError(t, e.saveProgress(t.Context(), turn, &p, progressAlways))
			job := seedInterrupted(t, e, turn)
			_, err := e.RecoverInterrupted(t.Context(), job)
			testkit.RequireNoError(t, err)
			status, err := e.Status(t.Context(), f.Owner, "assistant")
			testkit.RequireNoError(t, err)
			if !progress && (status.State != Waiting || status.WaitingReason != "runtime_recovery_exhausted") {
				t.Fatal("recovery loop not stopped", status)
			}
			if progress && status.State != Working {
				t.Fatal("progress did not reset recovery attempts")
			}
		})
	}
}

func TestRestartBeforeRecoveryHandoffReusesThePendingEvent(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	job := seedInterrupted(t, e, f.Turn)
	p := turnProgress{Inputs: []model.Input{{Role: model.System, Text: loopInstructions}, {Role: model.User, Text: "unfinished"}}}
	testkit.RequireNoError(t, e.saveProgress(t.Context(), f.Turn, &p, progressAlways))
	first := recoveryClaim(t, e, job)
	// A process can die after claiming recovery but before it writes its handoff.
	_, err := e.RecoverInterrupted(t.Context(), first.Job)
	testkit.RequireNoError(t, err)
	next, err := scheduler.NewQueue(e.store).Claim(t.Context(), time.Now().Add(time.Second))
	testkit.RequireNoError(t, err)
	if next.Job.Event.ID != first.Job.Event.ID {
		t.Fatal("nested a duplicate recovery before handoff")
	}
	_, err = e.Handle(t.Context(), Turn{Principal: next.Principal, Event: next.Job.Event})
	testkit.RequireNoError(t, err)
	if len(f.ModelRequests()) != 1 {
		t.Fatal("recovery did not execute")
	}
}

func TestRestartIneligibleTaskDoesNotResume(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-startup", true: "before-handle"}[late], func(t *testing.T) {
			f, _ := makeLoopFixture(t, nil)
			e := f.Runtime.(*Engine)
			job := seedInterrupted(t, e, f.Turn)
			e.options.RecoveryAllowed = func(context.Context, Turn) (bool, error) { return late, nil }
			if !late {
				_, err := e.RecoverInterrupted(t.Context(), job)
				testkit.RequireNoError(t, err)
				_, err = scheduler.NewQueue(e.store).Claim(t.Context(), time.Now().Add(time.Second))
				testkit.RequireCode(t, err, core.NotFound)
			} else {
				claim := recoveryClaim(t, e, job)
				e.options.RecoveryAllowed = func(context.Context, Turn) (bool, error) { return false, nil }
				_, err := e.Handle(t.Context(), Turn{Principal: claim.Principal, Event: claim.Job.Event})
				testkit.RequireCode(t, err, core.Canceled)
			}
			status, err := e.Status(t.Context(), f.Owner, "assistant")
			testkit.RequireNoError(t, err)
			if status.State != Idle || len(f.ModelRequests()) != 0 {
				t.Fatal("ineligible task continued", status)
			}
		})
	}
}

func TestRestartEarlyRecoveryFailureHasWaitingReason(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	job := seedInterrupted(t, e, f.Turn)
	claim := recoveryClaim(t, e, job)
	e.options.RecoveryAllowed = func(context.Context, Turn) (bool, error) {
		return false, core.NewError(core.Forbidden, "recovery access denied")
	}
	_, err := e.Handle(t.Context(), Turn{Principal: claim.Principal, Event: claim.Job.Event})
	if err == nil {
		t.Fatal("expected early access failure")
	}
	status, err := persistence.Load[Status](t.Context(), e.store, statusBucket, "owner/assistant")
	testkit.RequireNoError(t, err)
	if status.State != Waiting || status.WaitingReason != "forbidden" {
		t.Fatal("failed recovery left working state", status)
	}
}

func TestRestartCannotReadCheckpointOnlyFailsThatAssistant(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	job := seedInterrupted(t, e, f.Turn)
	// A checkpoint may be syntactically valid but have an invalid execution cursor.
	p := turnProgress{Inputs: []model.Input{{Role: model.System, Text: loopInstructions}}}
	testkit.RequireNoError(t, e.saveProgress(t.Context(), f.Turn, &p, progressAlways))
	cp, err := e.checkpoints.Load(t.Context(), "owner", "assistant")
	testkit.RequireNoError(t, err)
	state, err := decodeState(cp)
	testkit.RequireNoError(t, err)
	state.Current.Response = true
	cp.Context, _ = json.Marshal(state)
	cp.Sequence++
	testkit.RequireNoError(t, e.checkpoints.Save(t.Context(), cp, cp.Sequence-1))
	_, err = e.RecoverInterrupted(t.Context(), job)
	testkit.RequireNoError(t, err)
	status, err := e.Status(t.Context(), f.Owner, "assistant")
	testkit.RequireNoError(t, err)
	if status.State != Waiting || status.WaitingReason != "runtime_recovery_failed" {
		t.Fatal("corrupt state silently resumed", status)
	}
	if len(f.ModelRequests()) != 0 {
		t.Fatal("requested model with corrupt history")
	}
}
