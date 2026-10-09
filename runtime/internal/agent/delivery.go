package agent

import (
	"context"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
)

// pendingInterrupts and intakeEvents share scheduler eligibility and durable
// jobs. Specialized message intake retains its authorized reads/read receipts.
func (e *Engine) pendingInterrupts(ctx context.Context, t Turn, joined []core.ID) ([]core.ID, error) {
	ids, _, err := e.pendingInterruptsWithDeadline(ctx, t, joined)
	return ids, err
}

func (e *Engine) pendingInterruptsWithDeadline(ctx context.Context, t Turn, joined []core.ID) ([]core.ID, time.Time, error) {
	if t.Event.Kind == event.DreamDue {
		return nil, time.Time{}, nil
	}
	ids, err := e.pendingExpedite(ctx, t, joined)
	if err != nil {
		return nil, time.Time{}, err
	}
	jobs, next, err := scheduler.PendingWithDeadline(ctx, e.store, t.Principal.OwnerID, t.Event.AssistantID, t.Event.ID, e.options.Now(), true)
	if err != nil {
		return nil, time.Time{}, err
	}
	for _, job := range jobs {
		if job.Event.Kind != event.UserMessage {
			ids = append(ids, job.Event.ID)
		}
	}
	return ids, next, nil
}

// Watch only the model child. Register before reading the durable inbox, and
// consume notifications before each query so a concurrent commit cannot be lost.
func (e *Engine) watchInterrupts(ctx context.Context, t Turn, joined []core.ID, cancel context.CancelCauseFunc) {
	if t.Event.Kind == event.DreamDue {
		return
	}
	watch, err := e.store.Watch(event.AssistantInputSignal(t.Principal.OwnerID, t.Event.AssistantID))
	if err != nil {
		cancel(err)
		return
	}
	defer watch.Close()
	for {
		if ctx.Err() != nil {
			return
		}
		select {
		case <-watch.Changes():
		default:
		}
		ids, next, err := e.pendingInterruptsWithDeadline(ctx, t, joined)
		if err == nil && len(ids) > 0 {
			cancel(errExpedited)
			return
		}
		// A failed read must be retried even if no subsequent input is committed.
		if err != nil {
			next = e.options.Now().Add(time.Second)
		}
		var deadline <-chan time.Time
		var timer *time.Timer
		if !next.IsZero() {
			timer = time.NewTimer(next.Sub(e.options.Now()))
			deadline = timer.C
		}
		select {
		case <-ctx.Done():
		case <-watch.Done():
			if ctx.Err() == nil {
				cancel(core.NewError(core.Unavailable, "model input store closed"))
			}
		case <-watch.Changes():
		case <-deadline:
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

func (e *Engine) intakeEvents(ctx context.Context, t Turn, p *turnProgress) error {
	if t.Event.Kind == event.DreamDue {
		return nil
	}
	if err := e.intakeExpedite(ctx, t, p); err != nil {
		return err
	}
	next := *p
	next.Inputs = append([]model.Input{}, p.Inputs...)
	changed := false
	err := e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		jobs, err := scheduler.Pending(ctx, e.store, t.Principal.OwnerID, t.Event.AssistantID, t.Event.ID, e.options.Now(), false)
		if err != nil {
			return err
		}
		for _, job := range jobs {
			if job.Event.Kind == event.ContextCompressionRequested || job.Event.Kind == event.UserMessage {
				continue
			}
			var conversation core.ID
			if e.options.ConversationID != nil {
				conversation, err = e.options.ConversationID(ctx, t.Principal, t.Event.AssistantID)
				if err != nil {
					return err
				}
			}
			next.Inputs = append(next.Inputs, model.Input{Role: model.User, Text: eventText(job.Event, conversation)})
			if !next.PermissionPending {
				next.Finished = false
			}
			job.State, job.ExecutionID = scheduler.Done, t.Principal.ExecutionID
			job.Version++
			if err := event.SaveJob(ctx, tx, job); err != nil {
				return err
			}
			changed = true
		}
		if !changed {
			return nil
		}
		if err := checkContextSize(next.Inputs); err != nil {
			return err
		}
		return e.saveProgress(checkpointCause(ctx, "event_received"), t, &next)
	})
	if err == nil && changed {
		*p = next
	}
	return err
}

func (e *Engine) hasBoundaryEvents(ctx context.Context, t Turn) (bool, error) {
	if t.Event.Kind == event.DreamDue {
		return false, nil
	}
	jobs, err := scheduler.Pending(ctx, e.store, t.Principal.OwnerID, t.Event.AssistantID, t.Event.ID, e.options.Now(), false)
	if err != nil {
		return false, err
	}
	for _, job := range jobs {
		if job.Event.Kind != event.UserMessage {
			return true, nil
		}
	}
	return false, nil
}
