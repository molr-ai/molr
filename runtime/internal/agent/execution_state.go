package agent

import (
	"context"
	"encoding/json"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/prompt"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/media"
	"molr.ai/runtime/internal/persistence"
)

// Only the assistant checkpoint contains live context. Event records are
// revision references; they cannot restore an older copy over newer work.
type progressReference struct {
	Version     int
	AssistantID core.ID
	Revision    uint64
	Superseded  bool
}
type executionCursor struct {
	PermissionPending bool
	PublishAttempted  bool
	PublishReminded   bool
	Event             event.AssistantEvent
	ExecutionID       core.ID
	Definitions       []model.ToolDefinition
	Response          bool
	NextTool, Step    int
	Handled, Joined   []core.ID
	Finished          bool
}
type pendingEvidence struct {
	EventID, ExecutionID, TaskID, OperationID core.ID
	Call                                      *model.ToolCall `json:",omitempty"`
}

func cursor(t Turn, p turnProgress) *executionCursor {
	return &executionCursor{PermissionPending: p.PermissionPending, PublishAttempted: p.PublishAttempted, PublishReminded: p.PublishReminded, Event: t.Event, ExecutionID: t.Principal.ExecutionID, Definitions: p.Definitions, Response: p.Response != nil, NextTool: p.NextTool, Step: p.Step, Handled: p.Handled, Joined: p.Joined, Finished: p.Finished}
}
func (c executionCursor) turn() Turn {
	return Turn{Event: c.Event, Principal: core.Principal{OwnerID: c.Event.OwnerID, SubjectID: c.Event.AssistantID, Kind: core.ActorAssistant, ExecutionID: c.ExecutionID}}
}
func stateProgress(c Checkpoint, s privateContext) (turnProgress, error) {
	p := turnProgress{Revision: c.Sequence, Inputs: runtimeNotificationRoles(s.Inputs), Automatic: s.Automatic, Usage: s.Usage}
	if s.Current == nil {
		return p, nil
	}
	v := s.Current
	p.PermissionPending = v.PermissionPending
	p.PublishAttempted, p.PublishReminded = v.PublishAttempted, v.PublishReminded
	p.Definitions, p.NextTool, p.Step, p.Handled, p.Joined, p.Finished = v.Definitions, v.NextTool, v.Step, v.Handled, v.Joined, v.Finished
	if v.Response {
		for i := len(p.Inputs) - 1; i >= 0; i-- {
			if p.Inputs[i].Role == model.Assistant {
				copy := p.Inputs[i]
				p.Response = &copy
				break
			}
		}
		if p.Response == nil || p.NextTool < 0 || p.NextTool > len(p.Response.Calls) {
			return p, core.NewError(core.Internal, "invalid execution cursor")
		}
	}
	return p, nil
}
func decodeState(c Checkpoint) (privateContext, error) {
	var s privateContext
	if len(c.Context) == 0 {
		return s, nil
	}
	err := json.Unmarshal(c.Context, &s)
	return s, err
}
func (e *Engine) rejectSuperseded(ctx context.Context, t Turn) error {
	ref, err := persistence.Load[progressReference](ctx, e.store, progressBucket, persistence.Key(t.Principal.OwnerID, t.Event.ID))
	if err != nil && !persistence.Missing(err) {
		return err
	}
	if err == nil && ref.Version != 2 {
		return core.NewError(core.FailedPrecondition, "execution context storage upgrade required")
	}
	if ref.Version == 2 && (ref.Superseded || ref.AssistantID != t.Event.AssistantID) {
		return core.NewError(core.Canceled, "event was superseded by a newer execution")
	}
	return nil
}

type progressSaveMode bool

const (
	progressIfChanged progressSaveMode = false
	progressAlways    progressSaveMode = true
)

func (e *Engine) saveProgress(ctx context.Context, t Turn, p *turnProgress, mode progressSaveMode) error {
	// Dream batches have their own context and never publish assistant checkpoints.
	if t.Event.Kind == event.DreamDue {
		return e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return saveDreamProgress(ctx, tx, t, *p)
		})
	}
	var revision uint64
	err := e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := e.rejectSuperseded(ctx, t); err != nil {
			return err
		}
		if err := e.checkContextEpoch(ctx, t.Principal.OwnerID, t.Event.AssistantID, t.Event.CreatedAt); err != nil {
			return err
		}
		boundary, err := persistence.Load[event.InterruptBoundary](ctx, e.store, "agent.interrupt_boundaries", persistence.Key(t.Principal.OwnerID, t.Event.AssistantID))
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if boundary.Blocks(t.Event) {
			return core.NewError(core.Canceled, "execution predates stop")
		}
		cp, err := e.checkpoints.Load(ctx, t.Principal.OwnerID, t.Event.AssistantID)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		status, statusErr := persistence.Load[Status](ctx, e.store, statusBucket, persistence.Key(t.Principal.OwnerID, t.Event.AssistantID))
		if statusErr != nil && !persistence.Missing(statusErr) {
			return statusErr
		}
		if status.State == Stopped || status.State == Paused || (status.ExecutionID != "" && status.ExecutionID != t.Principal.ExecutionID) {
			return core.NewError(core.Canceled, "execution is no longer active")
		}
		if cp.Sequence != p.Revision {
			return core.NewError(core.Conflict, "assistant execution state advanced")
		}
		s, err := decodeState(cp)
		if err != nil {
			return err
		}
		s.Inputs, s.Automatic, s.Usage, s.Current = p.Inputs, p.Automatic, p.Usage, cursor(t, *p)
		// A returned tool failure can still represent uncertain external effects.
		if p.Response != nil && p.NextTool > 0 {
			call := p.Response.Calls[p.NextTool-1]
			ref := pendingCall(*s.Current, call)
			for i := len(p.Inputs) - 1; i >= 0; i-- {
				in := p.Inputs[i]
				if in.ToolOperationID != ref.OperationID {
					continue
				}
				var outcome struct {
					Error *tools.Failure `json:"error"`
				}
				if json.Unmarshal([]byte(in.Text), &outcome) == nil && outcome.Error != nil && outcome.Error.SideEffectsPossible {
					exists := false
					for _, known := range s.PendingEvidence {
						exists = exists || known.OperationID == ref.OperationID
					}
					if !exists {
						s.PendingEvidence = append(s.PendingEvidence, ref)
					}
				}
				break
			}
		}
		raw, err := json.Marshal(s)
		if err != nil {
			return err
		}
		next := Checkpoint{OwnerID: t.Principal.OwnerID, AssistantID: t.Event.AssistantID, ExecutionID: t.Principal.ExecutionID, LastEventID: t.Event.ID, FormatVersion: 1, Sequence: cp.Sequence + 1, SavedAt: e.options.Now().UTC(), Context: raw}
		if mode == progressAlways {
			err = e.checkpoints.Save(ctx, next, cp.Sequence)
		} else {
			var saved bool
			saved, err = e.checkpoints.SaveIfChanged(ctx, next, cp.Sequence)
			if err == nil && !saved {
				revision = cp.Sequence
				return nil
			}
		}
		if err != nil {
			return err
		}
		revision = next.Sequence
		return tx.Put(ctx, progressBucket, persistence.Key(t.Principal.OwnerID, t.Event.ID), progressReference{Version: 2, AssistantID: t.Event.AssistantID, Revision: revision})
	})
	if err == nil {
		p.Revision = revision
	}
	return err
}

func pendingCall(old executionCursor, call model.ToolCall) pendingEvidence {
	return pendingEvidence{EventID: old.Event.ID, ExecutionID: old.ExecutionID, TaskID: old.Event.TaskID, OperationID: stableID(string(old.Event.ID), "tool", call.ID), Call: &call}
}
func recoveredResult(call model.ToolCall, id core.ID, raw json.RawMessage) tools.Result {
	r := tools.Result{OperationID: id, Content: raw}
	if call.Name == "file_read" {
		var parsed struct{ Media []media.Part }
		if json.Unmarshal(raw, &parsed) == nil && len(parsed.Media) > 0 {
			r.Media = parsed.Media
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &fields) == nil {
				delete(fields, "Media")
				r.Content, _ = json.Marshal(fields)
			}
		}
	}
	return r
}
func (e *Engine) evidenceReply(ctx context.Context, t Turn, ref pendingEvidence, prepare bool) (model.Input, bool, error) {
	input := model.Input{ToolOperationID: ref.OperationID, Role: model.Tool, ReplyTo: ref.Call.ID}
	op, err := operation.NewRepository(e.store).Get(ctx, t.Principal.OwnerID, ref.OperationID)
	if err != nil && !persistence.Missing(err) {
		return input, false, err
	}
	if err == nil && (op.AssistantID != t.Event.AssistantID || op.ExecutionID != ref.ExecutionID || op.TaskID != ref.TaskID || op.Action != ref.Call.Name) {
		return input, false, core.NewError(core.Forbidden, "tool evidence identity mismatch")
	}
	state := "not_started"
	uncertain := false
	if err == nil && op.State != operation.Prepared {
		state = string(op.State)
		uncertain = op.State == operation.Running || op.State == operation.Unknown || op.State == operation.Canceled
	}
	if err == nil && op.State == operation.Succeeded && json.Valid(op.Result) {
		result := recoveredResult(*ref.Call, op.ID, op.Result)
		ids := conversationReadIDs(*ref.Call, result)
		if prepare && e.options.PrepareToolResult != nil {
			execution := core.Execution{Principal: t.Principal, AssistantID: t.Event.AssistantID, TaskID: t.Event.TaskID, OperationID: op.ID}
			result, err = e.options.PrepareToolResult(ctx, execution, result)
			if err != nil {
				return input, false, err
			}
		}
		if prepare || len(result.Content) <= 16<<10 {
			input.Text, input.Media, input.ReadMessageIDs = string(result.Content), result.Media, ids
			return input, false, nil
		}
	}
	raw, _ := json.Marshal(map[string]any{"ok": false, "operation_id": ref.OperationID, "error": map[string]any{"code": "canceled", "reason": "execution_handoff", "execution_state": state, "side_effects_possible": uncertain, "message": "The previous execution ended. Completed effects remain completed. Pending calls were not resumed. Inspect/reconcile uncertain operations before repeating them; a recorded successful result may still need loading."}})
	input.Text = string(raw)
	return input, uncertain || (err == nil && op.State == operation.Succeeded), nil
}
func (e *Engine) closePending(ctx context.Context, t Turn, s *privateContext, prepare bool) error {
	if s.Current == nil {
		return nil
	}
	old := *s.Current
	p, err := stateProgress(Checkpoint{}, *s)
	if err != nil {
		return err
	}
	if p.Response != nil {
		for _, call := range p.Response.Calls[p.NextTool:] {
			ref := pendingCall(old, call)
			reply, pending, err := e.evidenceReply(ctx, t, ref, prepare)
			if err != nil {
				return err
			}
			p.Inputs = append(p.Inputs, reply)
			if pending {
				s.PendingEvidence = append(s.PendingEvidence, ref)
			}
		}
	}
	s.Current.Handled = p.Handled
	// Handoff adds evidence; retain the provider usage baseline and estimate
	// only changed/new inputs. Only compaction resets the baseline.
	s.Inputs, s.Usage, s.Current = p.Inputs, p.Usage, nil
	return nil
}
func (e *Engine) refreshEvidence(ctx context.Context, t Turn, s *privateContext) error {
	remaining := []pendingEvidence{}
	for _, ref := range s.PendingEvidence {
		reply, pending, err := e.evidenceReply(ctx, t, ref, true)
		if err != nil {
			return err
		}
		if pending {
			remaining = append(remaining, ref)
			continue
		}
		found := false
		for i := range s.Inputs {
			if s.Inputs[i].Role == model.Tool && s.Inputs[i].ToolOperationID == ref.OperationID {
				s.Inputs[i] = reply
				found = true
				break
			}
		}
		if !found {
			s.Inputs = append(s.Inputs, model.Input{Role: model.User, Text: prompt.Text("reconciled", struct{ ID, Result string }{string(ref.OperationID), toolText(ref.Call.Name, reply.Text)})})
		}
	}
	s.PendingEvidence = remaining
	return nil
}

func (e *Engine) prepareExecution(ctx context.Context, t Turn) (Checkpoint, turnProgress, error) {
	cp, err := e.checkpoints.Load(ctx, t.Principal.OwnerID, t.Event.AssistantID)
	if err != nil && !persistence.Missing(err) {
		return cp, turnProgress{}, err
	}
	// Capture the version actually read, before reconciliation or the new event.
	loaded := cp
	exists := !persistence.Missing(err)
	if !exists {
		loaded.OwnerID, loaded.AssistantID = t.Principal.OwnerID, t.Event.AssistantID
		loaded.Context = json.RawMessage(`{"Inputs":[]}`)
	}
	e.options.Trace.Record(ctx, "context.build", "succeeded", "", map[string]any{"Checkpoint": loaded, "Exists": exists})
	s, err := decodeState(cp)
	if err != nil {
		return cp, turnProgress{}, err
	}
	if s.Current != nil && s.Current.Event.ID == t.Event.ID {
		if s.Current.ExecutionID != t.Principal.ExecutionID {
			return cp, turnProgress{}, core.NewError(core.Conflict, "execution identity changed")
		}
		p, err := stateProgress(cp, s)
		return cp, p, err
	}
	var old *executionCursor
	if s.Current != nil {
		old = s.Current
		if err = e.closePending(ctx, t, &s, true); err != nil {
			return cp, turnProgress{}, err
		}
		s.Inputs = append(s.Inputs, model.Input{Role: model.User, Text: prompt.Text("handoff", struct{ PreviousEvent, PreviousExecution, PreviousTask, CurrentEvent string }{string(old.Event.ID), string(old.ExecutionID), string(old.Event.TaskID), string(t.Event.ID)})})
	}
	if err = e.refreshEvidence(ctx, t, &s); err != nil {
		return cp, turnProgress{}, err
	}
	// Build the new event on the reconciled state, not on an event-local snapshot.
	raw, err := json.Marshal(s)
	if err != nil {
		return cp, turnProgress{}, err
	}
	base := cp
	base.OwnerID = t.Principal.OwnerID
	base.AssistantID = t.Event.AssistantID
	base.FormatVersion = 1
	base.Context = raw
	t.Checkpoint = &base
	inputs, err := e.builder.Build(ctx, t)
	if err != nil {
		return cp, turnProgress{}, err
	}
	p := turnProgress{Revision: cp.Sequence, Inputs: inputs, Automatic: s.Automatic, Usage: s.Usage}
	if t.Event.Recovery != nil {
		p.Step = t.Event.Recovery.Step
		if old != nil {
			p.PublishAttempted, p.PublishReminded = old.PublishAttempted, old.PublishReminded
		}
	}
	err = e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		current, err := e.checkpoints.Load(ctx, t.Principal.OwnerID, t.Event.AssistantID)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if current.Sequence != cp.Sequence {
			return core.NewError(core.Conflict, "execution state changed during handoff")
		}
		if old != nil {
			oldProgress := turnProgress{Handled: old.Handled, Joined: old.Joined}
			if err = releaseRetries(ctx, tx, t.Principal.OwnerID, t.Event.AssistantID, old.Event.ID); err != nil {
				return err
			}
			if err = e.discardPendingRecovery(ctx, tx, old.turn(), t.Event.ID); err != nil {
				return err
			}
			if err = e.finishExpedite(ctx, old.turn(), oldProgress); err != nil {
				return err
			}
			if err = tx.Put(ctx, progressBucket, persistence.Key(t.Principal.OwnerID, old.Event.ID), progressReference{Version: 2, AssistantID: t.Event.AssistantID, Revision: cp.Sequence, Superseded: true}); err != nil {
				return err
			}
			job, err := persistence.Load[scheduler.Job](ctx, e.store, "agent.jobs", persistence.Key(t.Principal.OwnerID, old.Event.ID))
			if err != nil && !persistence.Missing(err) {
				return err
			}
			if err == nil && job.State != scheduler.Done && job.State != scheduler.Canceled && job.State != scheduler.Interrupted {
				job.State = scheduler.Canceled
				job.FailureCode = core.Canceled
				job.Version++
				if err = event.SaveJob(ctx, tx, job); err != nil {
					return err
				}
			}
		}
		// Persist updated pending-evidence metadata along with the new inputs/cursor.
		s.Inputs, s.Automatic, s.Usage, s.Current = p.Inputs, p.Automatic, p.Usage, cursor(t, p)
		data, err := json.Marshal(s)
		if err != nil {
			return err
		}
		next := Checkpoint{OwnerID: t.Principal.OwnerID, AssistantID: t.Event.AssistantID, ExecutionID: t.Principal.ExecutionID, LastEventID: t.Event.ID, FormatVersion: 1, Sequence: cp.Sequence + 1, SavedAt: e.options.Now().UTC(), Context: data}
		if err = e.checkpoints.Save(checkpointCause(ctx, "event_received"), next, cp.Sequence); err != nil {
			return err
		}
		cp = next
		p.Revision = next.Sequence
		return tx.Put(ctx, progressBucket, persistence.Key(t.Principal.OwnerID, t.Event.ID), progressReference{Version: 2, AssistantID: t.Event.AssistantID, Revision: next.Sequence})
	})
	return cp, p, err
}
