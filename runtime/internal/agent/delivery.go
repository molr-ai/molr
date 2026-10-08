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
	if t.Event.Kind == event.DreamDue {
		return nil, nil
	}
	ids, err := e.pendingExpedite(ctx, t, joined)
	if err != nil {
		return nil, err
	}
	jobs, err := scheduler.Pending(ctx, e.store, t.Principal.OwnerID, t.Event.AssistantID, t.Event.ID, e.options.Now(), true)
	if err != nil {
		return nil, err
	}
	for _, job := range jobs {
		if job.Event.Kind != event.UserMessage {
			ids = append(ids, job.Event.ID)
		}
	}
	return ids, nil
}

// Watch only the cancellable model child, never the tool execution or the
// parent turn. Register before dispatch and also check synchronously to close
// the event-arrival race. Polling covers every transactional event producer.
func (e *Engine) watchInterrupts(ctx context.Context, t Turn, joined []core.ID, cancel context.CancelCauseFunc) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ids, err := e.pendingInterrupts(ctx, t, joined)
			if err == nil && len(ids) > 0 {
				cancel(errExpedited)
				return
			}
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
