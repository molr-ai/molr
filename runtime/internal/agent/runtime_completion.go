package agent

import (
	"context"
	"encoding/json"

	"molr.ai/runtime/internal/contextstore"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

func (e *Engine) commitTurn(runCtx context.Context, t Turn, key string, checkpoint Checkpoint, progress turnProgress) (Outcome, error) {
	state, waiting := Idle, ""
	handledKey := persistence.Key(t.Principal.OwnerID, stableID(string(t.Event.AssistantID), string(t.Event.ID)))
	if progress.PermissionPending {
		state = Waiting
		waiting = "permission_pending"
	}
	if !progress.Finished && progress.Step >= e.options.MaxSteps {
		state = Waiting
		waiting = "step_limit"
	}
	inputs, automatic := progress.Inputs, progress.Automatic

	e.options.Trace.Record(runCtx, "context.final", "succeeded", "", map[string]any{"Inputs": inputs})
	private, err := json.Marshal(privateContext{Inputs: contextHistory(inputs), Automatic: automatic, Usage: progress.Usage, Publish: progress.Publish})
	if err != nil {
		return Outcome{}, err
	}
	out := Outcome{Status: Status{AssistantID: t.Event.AssistantID, State: state, ActiveTaskID: t.Event.TaskID, ExecutionID: t.Principal.ExecutionID, WaitingReason: waiting, UpdatedAt: e.options.Now().UTC()}, Checkpoint: Checkpoint{OwnerID: t.Principal.OwnerID, AssistantID: t.Event.AssistantID, ExecutionID: t.Principal.ExecutionID, FormatVersion: 1, Sequence: checkpoint.Sequence + 1, Context: private, LastEventID: t.Event.ID, SavedAt: e.options.Now().UTC()}}
	// Dream files have their own durable commit plan. Never place filesystem
	// effects inside the status/checkpoint transaction: its rollback cannot undo files.
	if t.Event.Kind == event.DreamDue {
		if !progress.Finished {
			return Outcome{}, core.NewError(core.FailedPrecondition, "dream incomplete")
		}
		if err := e.authorize(runCtx, t.Principal, t.Event.AssistantID, policy.Execute); err != nil {
			return Outcome{}, err
		}
		if err := e.options.DreamCommit(runCtx, t.Principal, t.Event, progress.Inputs[len(progress.Inputs)-1].Text); err != nil {
			return Outcome{}, err
		}
	}
	err = e.store.Write(runCtx, func(ctx context.Context, tx persistence.Tx) error {
		if err := e.authorize(ctx, t.Principal, t.Event.AssistantID, policy.Execute); err != nil {
			return err
		}
		status, err := persistence.Load[Status](ctx, e.store, statusBucket, key)
		if err != nil {
			return err
		}
		if status.State == Stopped || status.State == Paused || status.ExecutionID != t.Principal.ExecutionID {
			return core.NewError(core.Canceled, "assistant stopped before checkpoint commit")
		}
		pending, err := e.pendingExpedite(ctx, t, progress.Joined)
		if err != nil {
			return err
		}
		if len(pending) > 0 && progress.Step < e.options.MaxSteps {
			return errPendingExpedite
		}
		pendingEvents, err := e.hasBoundaryEvents(ctx, t)
		if err != nil {
			return err
		}
		if pendingEvents && progress.Step < e.options.MaxSteps {
			return errPendingExpedite
		}
		out.Status.Context = status.Context
		out.Status.Compression = status.Compression
		if t.Event.Kind != event.DreamDue {
			if err := e.finishExpedite(ctx, t, progress); err != nil {
				return err
			}
		}
		if err := e.completeTurnContext(ctx, t, status, progress, &out); err != nil {
			return err
		}
		if err := e.saveStatus(ctx, tx, key, out.Status); err != nil {
			return err
		}
		if err := realtime.NewStore(e.store).Append(ctx, realtime.Visible{ID: stableID(string(t.Event.ID), "turn-end"), OwnerID: t.Principal.OwnerID, Topic: "assistant.state", Object: core.Object{Kind: "assistant", ID: t.Event.AssistantID}, CreatedAt: e.options.Now().UTC()}); err != nil {
			return err
		}
		// Idempotency receipts retain outcome metadata, never a historical copy
		// of live context. The current checkpoint is the only recovery authority.
		handled := out
		handled.Checkpoint.Context = nil
		if t.Event.Kind == event.DreamDue {
			if err := releaseRetries(ctx, tx, t.Principal.OwnerID, t.Event.AssistantID, t.Event.ID); err != nil {
				return err
			}
			if err := contextstore.Release(ctx, tx, storeReference(t.Principal.OwnerID, t.Event.AssistantID, "dream_progress", t.Event.ID)); err != nil {
				return err
			}
			if err := tx.Delete(ctx, progressBucket, persistence.Key(t.Principal.OwnerID, t.Event.ID)); err != nil && !persistence.Missing(err) {
				return err
			}
		}
		if err := tx.Put(ctx, "agent.handled", handledKey, handled); err != nil {
			return err
		}
		if e.options.Acknowledge != nil {
			return e.options.Acknowledge(ctx, t)
		}
		return nil
	})
	if err == nil && t.Event.Kind == event.DreamDue {
		e.options.Trace.Record(runCtx, "dream.batch_committed", "succeeded", "", map[string]any{"Sequence": out.Checkpoint.Sequence, "SavedAt": out.Checkpoint.SavedAt})
	}
	return out, err
}

// completeTurnContext joins the final transaction so the live checkpoint and
// outcome status advance together. Dream file commits have already completed.
func (e *Engine) completeTurnContext(ctx context.Context, t Turn, status Status, progress turnProgress, out *Outcome) error {
	if t.Event.Kind == event.DreamDue {
		if e.options.DreamActive != nil {
			active, err := e.options.DreamActive(ctx, t.Principal, t.Event)
			if err != nil {
				return err
			}
			if active {
				out.Status.State = Dreaming
				out.Status.DreamDeadline = status.DreamDeadline
				out.Status.DreamRunID = status.DreamRunID
			}
		}
	} else {
		latest, err := e.checkpoints.Load(ctx, t.Principal.OwnerID, t.Event.AssistantID)
		if err != nil {
			return err
		}
		if latest.Sequence != progress.Revision {
			return core.NewError(core.Conflict, "execution state advanced before completion")
		}
		saved, err := decodeState(latest)
		if err != nil {
			return err
		}
		saved.Inputs, saved.Automatic, saved.Usage, saved.Current = contextHistory(progress.Inputs), progress.Automatic, progress.Usage, nil
		out.Checkpoint.Context, err = json.Marshal(saved)
		if err != nil {
			return err
		}
		out.Checkpoint.Sequence = latest.Sequence + 1
		if err = e.checkpoints.Save(checkpointCause(ctx, "turn_completed"), out.Checkpoint, latest.Sequence); err != nil {
			return err
		}
	}

	return nil
}
