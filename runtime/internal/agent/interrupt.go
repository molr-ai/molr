package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/prompt"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

// Stop is the chat's interrupt button. It ends only the active turn, records
// user intent, fences earlier events, and returns the loop to Idle. Lifecycle("stop") is the separate
// persistent disable operation used by business lifecycle commands.
func (e *Engine) Stop(ctx context.Context, p core.Principal, id core.ID) error {
	if err := p.RequireOwner(); err != nil {
		return err
	}
	if err := e.authorize(ctx, p, id, policy.Write); err != nil {
		return err
	}
	key := persistence.Key(p.OwnerID, id)
	var stoppedExecution core.ID
	err := e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		status, err := persistence.Load[Status](ctx, e.store, statusBucket, key)
		if err != nil {
			return err
		}
		if status.State == Stopped || status.State == Paused {
			return core.NewError(core.FailedPrecondition, "persistently disabled assistant cannot be interrupted")
		}
		if status.State == Dreaming {
			stoppedExecution = status.ExecutionID
			if e.options.DreamStop == nil {
				return core.NewError(core.Unavailable, "dream stop unavailable")
			}
			if err := e.options.DreamStop(ctx, p, id); err != nil {
				return err
			}
			jobs, err := persistence.Select[scheduler.Job](ctx, e.store, "agent.jobs", persistence.Query{Where: []persistence.Filter{persistence.Eq("Event.OwnerID", p.OwnerID), persistence.Eq("Event.AssistantID", id), persistence.Eq("Event.Kind", event.DreamDue)}})
			if err != nil {
				return err
			}
			for _, job := range jobs {
				if job.Event.OwnerID == p.OwnerID && job.Event.AssistantID == id && job.Event.Kind == event.DreamDue && (job.State == scheduler.Running || job.State == scheduler.Queued || job.State == scheduler.Retry) {
					job.State = scheduler.Canceled
					job.Version++
					if err := event.SaveJob(ctx, tx, job); err != nil {
						return err
					}
				}
			}
			status.Compression = nil
			status.ModelRetry = nil
			status.State = Idle
			status.DreamDeadline = nil
			status.DreamRunID = ""
			status.TriggerMessageID = ""
			status.ExecutionID = ""
			status.UpdatedAt = e.options.Now().UTC()
			if err := e.saveStatus(ctx, tx, key, status); err != nil {
				return err
			}
			return realtime.NewStore(e.store).Append(ctx, realtime.Visible{ID: stableID(string(id), "dream-stop", status.UpdatedAt.String()), OwnerID: p.OwnerID, Topic: "assistant.dream_stopped", Object: core.Object{Kind: "assistant", ID: id}, CreatedAt: status.UpdatedAt})
		}
		random := make([]byte, 16)
		if _, err := rand.Read(random); err != nil {
			return err
		}
		control := event.AssistantEvent{ID: core.ID(hex.EncodeToString(random)), OwnerID: p.OwnerID, AssistantID: id, Kind: event.UserStopped, DeliveryMode: event.Interrupt, CreatedAt: e.options.Now().UTC()}
		var activeEvent core.ID
		if status.State == Working {
			stoppedExecution = status.ExecutionID
			control.ExecutionID = stoppedExecution
			control.TaskID = status.ActiveTaskID
			e.mu.Lock()
			active := e.active[key]
			e.mu.Unlock()
			if active.id == stoppedExecution {
				activeEvent = active.eventID
			}
		}
		jobs, err := persistence.Select[scheduler.Job](ctx, e.store, "agent.jobs", persistence.Query{Where: []persistence.Filter{persistence.Eq("Event.OwnerID", p.OwnerID), persistence.Eq("Event.AssistantID", id), {Field: "State", Op: "in", Value: []scheduler.State{scheduler.Queued, scheduler.Running, scheduler.Retry, scheduler.NeedsReview}}}})
		if err != nil {
			return err
		}
		for _, job := range jobs {
			if job.Event.OwnerID == p.OwnerID && job.Event.AssistantID == id && (job.State == scheduler.Running || job.State == scheduler.Retry) && job.ExecutionID == stoppedExecution && stoppedExecution != "" {
				activeEvent = job.Event.ID
			}
		}
		// Stop and message/event creation serialize on the same store. Everything
		// already queued belongs before this boundary, even if not yet claimed.
		boundary := event.InterruptBoundary{At: control.CreatedAt}
		if e.options.ConversationID != nil && e.options.Conversations != nil {
			chatID, err := e.options.ConversationID(ctx, p, id)
			if err != nil {
				return err
			}
			chat, err := e.options.Conversations.Get(ctx, p, chatID)
			if err != nil {
				return err
			}
			boundary.ConversationID = chatID
			boundary.ThroughSequence = chat.LastSequence
		}
		if err := tx.Put(ctx, "agent.interrupt_boundaries", key, boundary); err != nil {
			return err
		}
		for _, job := range jobs {
			if job.Event.OwnerID != p.OwnerID || job.Event.AssistantID != id || job.Event.Kind == event.UserStopped {
				continue
			}
			if job.State == scheduler.Done || job.State == scheduler.Failed || job.State == scheduler.Canceled {
				continue
			}
			job.State = scheduler.Canceled
			job.Version++
			if err := event.SaveJob(ctx, tx, job); err != nil {
				return err
			}
		}
		if err := event.NewWriter(e.store).AppendAssistant(ctx, control); err != nil {
			return err
		}
		if err := e.interruptCheckpoint(ctx, p, id, activeEvent, control); err != nil {
			return err
		}
		if err := tx.Delete(ctx, expediteBucket, key); err != nil && !persistence.Missing(err) {
			return err
		}

		status.Compression = nil
		status.ModelRetry = nil
		status.State = Idle
		status.TriggerMessageID = ""
		status.ExecutionID = ""
		status.ActiveTaskID = ""
		status.WaitingReason = ""
		status.UpdatedAt = control.CreatedAt
		if err := e.saveStatus(ctx, tx, key, status); err != nil {
			return err
		}
		return realtime.NewStore(e.store).Append(ctx, realtime.Visible{ID: control.ID, OwnerID: p.OwnerID, Topic: "assistant.user_stopped", Object: core.Object{Kind: "assistant", ID: id}, CreatedAt: control.CreatedAt})
	})
	if err != nil {
		return err
	}
	e.mu.Lock()
	active := e.active[key]
	e.mu.Unlock()
	// A later event can already be waiting on the execution gate. The old
	// request must never cancel a new execution that won the gate first.
	if stoppedExecution != "" && active.id == stoppedExecution && active.cancel != nil {
		active.cancel()
	}
	return nil
}

func (e *Engine) interruptCheckpoint(ctx context.Context, p core.Principal, id, activeEvent core.ID, control event.AssistantEvent) error {
	checkpoint, err := e.checkpoints.Load(ctx, p.OwnerID, id)
	if err != nil && !persistence.Missing(err) {
		return err
	}
	saved := privateContext{}
	if len(checkpoint.Context) > 0 {
		if err := json.Unmarshal(checkpoint.Context, &saved); err != nil {
			return err
		}
	}
	if saved.Current == nil && activeEvent != "" {
		// Compatibility with a pre-upgrade in-flight event snapshot.
		legacy, loadErr := persistence.Load[turnProgress](ctx, e.store, progressBucket, persistence.Key(p.OwnerID, activeEvent))
		if loadErr != nil && !persistence.Missing(loadErr) {
			return loadErr
		}
		if len(legacy.Inputs) > 0 {
			source, err := persistence.Load[event.AssistantEvent](ctx, e.store, "agent.events", persistence.Key(p.OwnerID, activeEvent))
			if err != nil {
				return err
			}
			if source.Kind != event.DreamDue {
				saved = privateContext{Inputs: legacy.Inputs, Automatic: legacy.Automatic, Usage: legacy.Usage, Current: cursor(Turn{Event: source, Principal: core.Principal{OwnerID: p.OwnerID, SubjectID: id, Kind: core.ActorAssistant, ExecutionID: control.ExecutionID}}, legacy)}
			}
		}
	}
	if saved.Current != nil {
		old := saved.Current
		if err = e.closePending(ctx, old.turn(), &saved, false); err != nil {
			return err
		}
		if err = e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, progressBucket, persistence.Key(p.OwnerID, old.Event.ID), progressReference{Version: 2, AssistantID: id, Revision: checkpoint.Sequence, Superseded: true})
		}); err != nil {
			return err
		}
	}
	if len(saved.Inputs) == 0 {
		saved.Inputs = []model.Input{{Role: model.System, Text: loopInstructions}}
	}
	saved.Inputs = append(saved.Inputs, model.Input{Role: model.User, Text: prompt.Text("control", eventPayload(control))})
	raw, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	next := Checkpoint{OwnerID: p.OwnerID, AssistantID: id, ExecutionID: control.ID, FormatVersion: 1, Sequence: checkpoint.Sequence + 1, Context: raw, LastEventID: control.ID, SavedAt: control.CreatedAt}
	return e.checkpoints.Save(checkpointCause(ctx, "user_stopped"), next, checkpoint.Sequence)
}
