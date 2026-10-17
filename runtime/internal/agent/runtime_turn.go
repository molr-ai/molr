package agent

import (
	"context"
	"errors"
	"log"
	"time"

	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

func (e *Engine) Handle(ctx context.Context, t Turn) (outcome Outcome, resultErr error) {
	if t.Event.Recovery != nil {
		defer func() { e.finishRecoveryFailure(ctx, t, resultErr) }()
	}
	if err := t.Validate(); err != nil {
		return Outcome{}, err
	}
	if err := e.checkEnvironment(ctx); err != nil {
		return Outcome{}, err
	}
	if t.Event.Kind == event.UserStopped {
		return Outcome{}, core.NewError(core.FailedPrecondition, "control events do not start execution")
	}
	if err := e.authorize(ctx, t.Principal, t.Event.AssistantID, policy.Execute); err != nil {
		return Outcome{}, err
	}
	if e.options.Dependencies.Models == nil || e.options.Dependencies.Tools == nil || e.options.Dependencies.Budget == nil || e.options.ResolveModel == nil || e.options.Estimate == nil {
		return Outcome{}, core.NewError(core.FailedPrecondition, "agent execution dependencies unavailable")
	}
	key := persistence.Key(t.Principal.OwnerID, t.Event.AssistantID)
	e.mu.Lock()
	gate := e.gates[key]
	if gate == nil {
		gate = make(chan struct{}, 1)
		e.gates[key] = gate
	}
	e.mu.Unlock()
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		return Outcome{}, persistence.CheckContext(ctx)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	e.mu.Lock()
	e.active[key] = activeExecution{id: t.Principal.ExecutionID, eventID: t.Event.ID, cancel: cancel}
	e.mu.Unlock()
	defer func() { e.mu.Lock(); delete(e.active, key); e.mu.Unlock() }()
	previous, err := e.startTurn(runCtx, t, key)
	if err != nil || previous.Checkpoint.LastEventID != "" {
		return previous, err
	}
	runCtx = e.options.Trace.Begin(runCtx, t.Principal.OwnerID, t.Event.AssistantID, t.Event.ID, t.Principal.ExecutionID, string(t.Event.Kind), t.Attempt, t.Event)
	return e.executeTurn(ctx, runCtx, t, key)
}
func (e *Engine) startTurn(ctx context.Context, t Turn, key string) (Outcome, error) {
	handledKey := persistence.Key(t.Principal.OwnerID, stableID(string(t.Event.AssistantID), string(t.Event.ID)))
	var previous Outcome
	err := e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		err := tx.Get(ctx, "agent.handled", handledKey, &previous)
		if err == nil {
			if e.options.Acknowledge != nil {
				return e.options.Acknowledge(ctx, t)
			}
			return nil
		}
		if !persistence.Missing(err) {
			return err
		}
		boundary, boundaryErr := persistence.Load[event.InterruptBoundary](ctx, e.store, "agent.interrupt_boundaries", key)
		if boundaryErr != nil && !persistence.Missing(boundaryErr) {
			return boundaryErr
		}
		if boundary.Blocks(t.Event) {
			return core.NewError(core.Canceled, "event predates manual stop")
		}
		if err := e.checkContextEpoch(ctx, t.Principal.OwnerID, t.Event.AssistantID, t.Event.CreatedAt); err != nil {
			return err
		}
		status, err := persistence.Load[Status](ctx, e.store, statusBucket, key)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if status.State == Stopped || status.State == Paused {
			return core.NewError(core.FailedPrecondition, "assistant execution is stopped")
		}
		if err := e.checkRecovery(ctx, t); err != nil {
			return err
		}
		if err := e.rejectSuperseded(ctx, t); err != nil {
			return err
		}
		if err := e.startTurnTiming(ctx, t); err != nil {
			return err
		}
		state := Working
		if t.Event.Kind == event.DreamDue {
			state = Dreaming
		}
		status = Status{Context: status.Context, AssistantID: t.Event.AssistantID, State: state, ActiveTaskID: t.Event.TaskID, ExecutionID: t.Principal.ExecutionID, UpdatedAt: e.options.Now().UTC()}
		if t.Event.Kind == event.UserMessage {
			status.TriggerMessageID = t.Event.MessageID
		}
		if err := realtime.NewStore(e.store).Append(ctx, realtime.Visible{ID: stableID(string(t.Event.ID), "turn-start", status.UpdatedAt.String()), OwnerID: t.Principal.OwnerID, Topic: "assistant.state", Object: core.Object{Kind: "assistant", ID: t.Event.AssistantID}, CreatedAt: status.UpdatedAt}); err != nil {
			return err
		}
		return e.saveStatus(ctx, tx, key, status)
	})
	if err != nil {
		return Outcome{}, err
	}
	return previous, nil
}

// Cleanup failures are reported separately: wrapping or joining them can change
// scheduler classification of the original error (including cancellation).
func (e *Engine) finishTurn(ctx, runCtx context.Context, t Turn, key string, outcome Outcome, resultErr *error) {
	e.finishFailedTurn(ctx, runCtx, t, key, *resultErr)
	e.finishTurnTrace(ctx, runCtx, t, outcome, *resultErr)
	if scheduler.IsDeferred(*resultErr) {
		return
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := e.finishTurnTiming(finishCtx, t); err != nil {
		if *resultErr == nil {
			*resultErr = err
		} else {
			log.Printf("agent turn timing cleanup failed: assistant=%s execution=%s cleanup=%v", t.Event.AssistantID, t.Principal.ExecutionID, err)
		}
	}
}
func (e *Engine) finishFailedTurn(ctx, runCtx context.Context, t Turn, key string, resultErr error) {
	if resultErr == nil || scheduler.IsDeferred(resultErr) || errors.Is(context.Cause(ctx), scheduler.ErrRuntimeInterrupted) {
		return
	}
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(runCtx), 5*time.Second)
	defer finishCancel()
	err := e.store.Write(finishCtx, func(ctx context.Context, tx persistence.Tx) error {
		status, err := persistence.Load[Status](ctx, e.store, statusBucket, key)
		if err != nil {
			return err
		}
		if (status.State != Working && status.State != Dreaming) || status.ExecutionID != t.Principal.ExecutionID {
			return nil
		}
		status.State = Waiting
		if status.Compression != nil && status.Compression.State == "running" {
			status.Compression.State = "failed"
		}
		status.ModelRetry = nil
		status.TriggerMessageID = ""
		if t.Event.Kind == event.DreamDue {
			status.State = Idle
			status.DreamDeadline = nil
			status.DreamRunID = ""
		}
		status.WaitingReason = string(core.CodeOf(resultErr))
		if reason := core.ReasonOf(resultErr); reason != "" {
			status.WaitingReason = string(reason)
		}
		status.UpdatedAt = e.options.Now().UTC()
		return e.saveStatus(ctx, tx, key, status)
	})

	if err != nil {
		log.Printf("agent turn status cleanup failed: assistant=%s execution=%s cause=%s cleanup=%v", t.Event.AssistantID, t.Principal.ExecutionID, core.CodeOf(resultErr), err)
		e.options.Trace.Record(finishCtx, "turn.cleanup_failed", "failed", "", map[string]any{"Stage": "status", "Error": err.Error()})
	}
}
func (e *Engine) finishTurnTrace(ctx, runCtx context.Context, t Turn, outcome Outcome, resultErr error) {
	state, reason := "succeeded", ""
	if resultErr != nil {
		state = "failed"
		reason = string(core.CodeOf(resultErr))
		if r := core.ReasonOf(resultErr); r != "" {
			reason = string(r)
		}
		if core.CodeOf(resultErr) == core.Canceled {
			state = "canceled"
		}
	}
	if errors.Is(context.Cause(ctx), scheduler.ErrRuntimeInterrupted) && resultErr != nil {
		state, reason = "interrupted", "runtime_stopped"
	}
	if scheduler.IsDeferred(resultErr) {
		state = "retrying"
	}
	if resultErr == nil && outcome.Status.State == Waiting {
		state = "waiting"
		reason = outcome.Status.WaitingReason
	}
	traceCtx := runCtx
	if t.Event.Kind == event.DreamDue {
		var traceCancel context.CancelFunc
		traceCtx, traceCancel = context.WithTimeout(context.WithoutCancel(runCtx), time.Second)
		defer traceCancel()
	}
	if t.Event.Kind == event.DreamDue && resultErr != nil && !scheduler.IsDeferred(resultErr) {
		e.options.Trace.Record(traceCtx, "dream.ended", state, "", map[string]any{"Reason": reason, "FileChanges": "successful file operations retained", "Error": resultErr.Error()})
	}
	e.options.Trace.Finish(traceCtx, state, reason)
}
