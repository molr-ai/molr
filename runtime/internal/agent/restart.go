package agent

import (
	"context"
	"errors"
	"time"

	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
)

// RecoverInterrupted runs under the Runtime's exclusive lock, before workers.
// It creates a new trigger, never replays the interrupted tool/model queue.
func (e *Engine) RecoverInterrupted(ctx context.Context, job scheduler.Job) (bool, error) {
	if job.Event.Kind == event.DreamDue {
		return false, nil
	}
	if job.ExecutionID == "" {
		return true, nil
	}
	err := e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		key := persistence.Key(job.Event.OwnerID, job.Event.ID)
		current, err := persistence.Load[scheduler.Job](ctx, tx.Store(), "agent.jobs", key)
		if err != nil {
			return err
		}
		if current.Version != job.Version || current.State != scheduler.Running {
			return nil
		}
		job = current
		assistantKey := persistence.Key(job.Event.OwnerID, job.Event.AssistantID)
		status, err := persistence.Load[Status](ctx, tx.Store(), statusBucket, assistantKey)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		boundary, err := persistence.Load[event.InterruptBoundary](ctx, tx.Store(), "agent.interrupt_boundaries", assistantKey)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		cancel := func() error {
			job.State, job.FailureCode = scheduler.Canceled, core.Canceled
			job.Version++
			return event.SaveJob(ctx, tx, job)
		}
		if boundary.Blocks(job.Event) || status.State == Stopped || status.State == Paused {
			return cancel()
		}
		t := Turn{Event: job.Event, Principal: core.Principal{OwnerID: job.Event.OwnerID, SubjectID: job.Event.AssistantID, Kind: core.ActorAssistant, ExecutionID: job.ExecutionID}}
		if e.options.RecoveryAllowed != nil {
			allowed, err := e.options.RecoveryAllowed(ctx, t)
			if err != nil {
				return err
			}
			if !allowed {
				if err := cancel(); err != nil {
					return err
				}
				if status.State == Working && status.ExecutionID == job.ExecutionID {
					status.State, status.WaitingReason, status.ExecutionID, status.ActiveTaskID = Idle, "", "", ""
					status.ModelRetry, status.TriggerMessageID = nil, ""
					status.UpdatedAt = e.options.Now().UTC()
					return e.saveStatus(ctx, tx, assistantKey, status)
				}
				return nil
			}
		}
		if err := e.checkContextEpoch(ctx, job.Event.OwnerID, job.Event.AssistantID, job.Event.CreatedAt); err != nil {
			if core.CodeOf(err) == core.Canceled {
				return cancel()
			}
			return err
		}
		if err := e.rejectSuperseded(ctx, t); err != nil {
			if core.CodeOf(err) == core.Canceled {
				return cancel()
			}
			return err
		}
		var handled Outcome
		if err := tx.Get(ctx, "agent.handled", persistence.Key(job.Event.OwnerID, stableID(string(job.Event.AssistantID), string(job.Event.ID))), &handled); err == nil {
			job.State = scheduler.Done
			job.Version++
			return event.SaveJob(ctx, tx, job)
		} else if !persistence.Missing(err) {
			return err
		}
		fail := func(reason string) error {
			job.State, job.FailureCode = scheduler.Failed, core.Unavailable
			job.Version++
			if err := event.SaveJob(ctx, tx, job); err != nil {
				return err
			}
			status.AssistantID, status.State, status.WaitingReason = job.Event.AssistantID, Waiting, reason
			status.ModelRetry, status.TriggerMessageID = nil, ""
			status.UpdatedAt = e.options.Now().UTC()
			return e.saveStatus(ctx, tx, assistantKey, status)
		}
		cp, err := e.checkpoints.Load(ctx, job.Event.OwnerID, job.Event.AssistantID)
		if err != nil && !persistence.Missing(err) {
			return fail("runtime_recovery_failed")
		}
		saved, err := decodeState(cp)
		if err != nil {
			return fail("runtime_recovery_failed")
		}
		if _, err := stateProgress(cp, saved); err != nil {
			return fail("runtime_recovery_failed")
		}
		ref, refErr := persistence.Load[progressReference](ctx, tx.Store(), progressBucket, key)
		if refErr != nil && !persistence.Missing(refErr) {
			return refErr
		}
		if persistence.Missing(refErr) && (status.ExecutionID != job.ExecutionID || job.Event.Recovery != nil) {
			// Claim may commit before Handle or its handoff checkpoint. The trigger has
			// not entered the context, so retain it instead of nesting another resume.
			if job.Event.Recovery != nil {
				if err := e.checkRecovery(ctx, t); err != nil {
					if core.CodeOf(err) == core.Canceled {
						return cancel()
					}
					return err
				}
			}
			if job.Attempts >= 3 {
				return fail("runtime_recovery_exhausted")
			}
			job.State = scheduler.Retry
			job.InterruptedAt = nil
			job.Version++
			return event.SaveJob(ctx, tx, job)
		}
		if ref.Superseded || (saved.Current != nil && (saved.Current.Event.ID != job.Event.ID || saved.Current.ExecutionID != job.ExecutionID)) {
			return cancel()
		}
		r := &event.Recovery{PreviousEventID: job.Event.ID, PreviousExecutionID: job.ExecutionID, SourceKind: job.Event.Kind, SourceCreatedAt: job.Event.CreatedAt, SourceMessageSequence: job.Event.MessageSequence, Reason: "runtime_interrupted", InterruptedAt: job.InterruptedAt, Attempts: 1}
		if r.InterruptedAt != nil {
			r.Reason = "runtime_stopped"
		}
		if saved.Current != nil {
			r.Step = saved.Current.Step
		}
		if prior := job.Event.Recovery; prior != nil {
			r.SourceKind, r.SourceCreatedAt, r.SourceMessageSequence = prior.SourceKind, prior.SourceCreatedAt, prior.SourceMessageSequence
			r.Step = max(r.Step, prior.Step)
			r.Attempts = prior.Attempts + 1
			if saved.Current != nil && (saved.Current.Step > prior.Step || saved.Current.NextTool > 0) {
				r.Attempts = 1
			}
		}
		if r.Attempts > 3 {
			return fail("runtime_recovery_exhausted")
		}
		resume := event.AssistantEvent{ID: stableID("runtime-resume", string(job.Event.ID), string(job.ExecutionID)), OwnerID: job.Event.OwnerID, AssistantID: job.Event.AssistantID, TaskID: job.Event.TaskID, Kind: event.ResumeRequested, DeliveryMode: event.WhenIdle, CreatedAt: e.options.Now().UTC(), Recovery: r}
		if err := event.NewWriter(tx.Store()).AppendAssistant(ctx, resume); err != nil {
			return err
		}
		next, err := persistence.Load[scheduler.Job](ctx, tx.Store(), "agent.jobs", persistence.Key(resume.OwnerID, resume.ID))
		if err != nil {
			return err
		}
		// Retain assistant ordering and any provider Retry-After without dispatching
		// the old request. Unclaimed resume jobs have no execution identity yet.
		next.State = scheduler.Retry
		if job.NotBefore.After(next.NotBefore) {
			next.NotBefore = job.NotBefore
		}
		if err := event.SaveJob(ctx, tx, next); err != nil {
			return err
		}
		job.State, job.RecoveryEventID, job.FailureCode = scheduler.Interrupted, resume.ID, ""
		job.Version++
		if err := event.SaveJob(ctx, tx, job); err != nil {
			return err
		}
		status.AssistantID, status.State, status.WaitingReason = job.Event.AssistantID, Working, ""
		status.ModelRetry, status.TriggerMessageID = nil, ""
		status.UpdatedAt = resume.CreatedAt
		return e.saveStatus(ctx, tx, assistantKey, status)
	})
	return true, err
}

// A new event's timestamp must not bypass a stop/reset or a newer handoff.
// The source link is checked again at claim time, not only during startup.
func (e *Engine) checkRecovery(ctx context.Context, t Turn) error {
	r := t.Event.Recovery
	if r == nil {
		return nil
	}
	job, err := persistence.Load[scheduler.Job](ctx, e.store, "agent.jobs", persistence.Key(t.Principal.OwnerID, r.PreviousEventID))
	if err != nil {
		return err
	}
	if job.State != scheduler.Interrupted || job.RecoveryEventID != t.Event.ID || job.ExecutionID != r.PreviousExecutionID || job.Event.AssistantID != t.Event.AssistantID || job.Event.TaskID != t.Event.TaskID {
		return core.NewError(core.Canceled, "recovery source no longer active")
	}
	if e.options.RecoveryAllowed != nil {
		allowed, err := e.options.RecoveryAllowed(ctx, t)
		if err != nil {
			return err
		}
		if !allowed {
			return core.NewError(core.Canceled, "task no longer eligible for recovery")
		}
	}
	if err := e.checkContextEpoch(ctx, t.Principal.OwnerID, t.Event.AssistantID, r.SourceCreatedAt); err != nil {
		return err
	}
	cp, err := e.checkpoints.Load(ctx, t.Principal.OwnerID, t.Event.AssistantID)
	if err != nil && !persistence.Missing(err) {
		return err
	}
	s, err := decodeState(cp)
	if err != nil {
		return err
	}
	if (s.Current != nil && s.Current.Event.ID != r.PreviousEventID && s.Current.Event.ID != t.Event.ID) || (s.Current == nil && cp.LastEventID != "" && cp.LastEventID != r.PreviousEventID && cp.LastEventID != t.Event.ID && cp.SavedAt.After(job.Event.CreatedAt)) {
		return core.NewError(core.Canceled, "recovery superseded by newer work")
	}
	return nil
}

func (e *Engine) discardPendingRecovery(ctx context.Context, tx persistence.Tx, old Turn, next core.ID) error {
	job, err := persistence.Load[scheduler.Job](ctx, tx.Store(), "agent.jobs", persistence.Key(old.Principal.OwnerID, old.Event.ID))
	if persistence.Missing(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if job.RecoveryEventID == "" || job.RecoveryEventID == next {
		return nil
	}
	pending, err := persistence.Load[scheduler.Job](ctx, tx.Store(), "agent.jobs", persistence.Key(old.Principal.OwnerID, job.RecoveryEventID))
	if err != nil {
		return err
	}
	if pending.State != scheduler.Queued && pending.State != scheduler.Retry {
		return nil
	}
	pending.State, pending.FailureCode = scheduler.Canceled, core.Canceled
	pending.Version++
	return event.SaveJob(ctx, tx, pending)
}

// Initial authorization/environment checks precede Handle's normal error cleanup.
// Recovery must not remain visibly working if one of those checks rejects it.
func (e *Engine) finishRecoveryFailure(ctx context.Context, t Turn, cause error) {
	if cause == nil || scheduler.IsDeferred(cause) || errors.Is(context.Cause(ctx), scheduler.ErrRuntimeInterrupted) || t.Validate() != nil {
		return
	}
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = e.store.Write(finish, func(ctx context.Context, tx persistence.Tx) error {
		key := persistence.Key(t.Principal.OwnerID, t.Event.AssistantID)
		status, err := persistence.Load[Status](ctx, tx.Store(), statusBucket, key)
		if err != nil {
			return err
		}
		if status.State != Working || (status.ExecutionID != t.Principal.ExecutionID && status.ExecutionID != t.Event.Recovery.PreviousExecutionID) {
			return nil
		}
		status.ModelRetry, status.TriggerMessageID = nil, ""
		status.State, status.WaitingReason = Waiting, string(core.CodeOf(cause))
		if reason := core.ReasonOf(cause); reason != "" {
			status.WaitingReason = string(reason)
		}
		if core.CodeOf(cause) == core.Canceled {
			status.State, status.WaitingReason, status.ExecutionID, status.ActiveTaskID = Idle, "", "", ""
		}
		status.UpdatedAt = e.options.Now().UTC()
		return e.saveStatus(ctx, tx, key, status)
	})
}
