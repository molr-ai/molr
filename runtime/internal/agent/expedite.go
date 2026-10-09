package agent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/prompt"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

const expediteBucket = "agent.expedite"
const progressBucket = "agent.turn_progress"

var errExpedited = errors.New("model request superseded by expedited messages")
var errPendingExpedite = errors.New("expedited messages arrived before checkpoint commit")

type ExpediteRequest struct {
	Mutation        core.Mutation
	ConversationID  core.ID
	ThroughSequence uint64
}
type ExpediteStatus struct {
	ThroughSequence uint64
	State           string // pending, waiting_tool, waiting, read, not_running, not_needed
}
type expediteRecord struct {
	ConversationID  core.ID
	ThroughSequence uint64
	MessageIDs      []core.ID
}
type expediteReceipt struct {
	State           string `json:",omitempty"`
	ConversationID  core.ID
	ThroughSequence uint64
}
type turnProgress struct {
	PermissionPending bool
	PublishAttempted  bool
	PublishReminded   bool
	Revision          uint64        `json:",omitempty"`
	Usage             *contextUsage `json:",omitempty"`
	Definitions       []model.ToolDefinition
	Response          *model.Input
	NextTool          int
	Handled           []core.ID
	Finished          bool
	Inputs            []model.Input
	Automatic         *automaticContext
	Step              int
	Joined            []core.ID
}

func (e *Engine) Expedite(ctx context.Context, p core.Principal, id core.ID, r ExpediteRequest) (ExpediteStatus, error) {
	if err := p.RequireOwner(); err != nil {
		return ExpediteStatus{}, err
	}
	if err := e.authorize(ctx, p, id, policy.Write); err != nil {
		return ExpediteStatus{}, err
	}
	if err := r.Mutation.Validate(false); err != nil {
		return ExpediteStatus{}, err
	}
	if r.ConversationID.Validate() != nil || r.ThroughSequence == 0 {
		return ExpediteStatus{}, core.Invalid("expedite", "conversation and message sequence required")
	}
	if e.options.Conversations == nil {
		return ExpediteStatus{}, core.NewError(core.FailedPrecondition, "conversation delivery unavailable")
	}
	key := persistence.Key(p.OwnerID, id)
	receiptKey := persistence.Key(p.OwnerID, stableID(string(id), r.Mutation.IdempotencyKey))
	changed := false
	var expeditedExecution core.ID
	result := ExpediteStatus{ThroughSequence: r.ThroughSequence, State: "pending"}
	err := e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		c, err := e.options.Conversations.Get(ctx, p, r.ConversationID)
		if err != nil {
			return err
		}
		if c.PeerID != id || c.Archived {
			return core.NewError(core.Forbidden, "active assistant conversation required")
		}
		if r.ThroughSequence > c.LastSequence {
			return core.Invalid("through_sequence", "message sequence does not exist")
		}
		var receipt expediteReceipt
		err = tx.Get(ctx, "agent.expedite_receipts", receiptKey, &receipt)
		if err == nil {
			if receipt.ConversationID != r.ConversationID || receipt.ThroughSequence != r.ThroughSequence {
				return core.NewError(core.Conflict, "idempotency key reused")
			}
			if receipt.State != "" {
				result.State = receipt.State
			}
			return nil
		}
		if !persistence.Missing(err) {
			return err
		}
		status, err := persistence.Load[Status](ctx, e.store, statusBucket, key)
		if err != nil {
			return err
		}
		if status.State == Dreaming {
			return core.NewError(core.FailedPrecondition, "cannot expedite while assistant is dreaming")
		}
		if status.State != Working {
			result.State = "not_running"
			return nil
		}
		expeditedExecution = status.ExecutionID
		record, err := persistence.Load[expediteRecord](ctx, e.store, expediteBucket, key)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		record.ConversationID = r.ConversationID
		record.ThroughSequence = max(record.ThroughSequence, r.ThroughSequence)
		boundary, err := persistence.Load[event.InterruptBoundary](ctx, e.store, "agent.interrupt_boundaries", key)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		q := conversation.MessageQuery{ConversationID: r.ConversationID, AfterSequence: boundary.ThroughSequence, Page: core.PageRequest{Limit: 200}}
		bytes := 0
		eligible := false
		for {
			page, err := e.options.Conversations.Messages(ctx, p, q)
			if err != nil {
				return err
			}
			for _, m := range page.Items {
				if m.Sequence > r.ThroughSequence {
					break
				}
				if m.Sender != conversation.User || m.ReadAt != nil || m.ID == status.TriggerMessageID {
					continue
				}
				source := scheduler.UserMessageEvent(p.OwnerID, id, m)
				sourceKey := persistence.Key(p.OwnerID, source.ID)
				stored, err := persistence.Load[event.AssistantEvent](ctx, e.store, "agent.events", sourceKey)
				if err != nil && !persistence.Missing(err) {
					return err
				}
				if err == nil {
					stored.DeliveryMode = event.Interrupt
					if err := tx.Put(ctx, "agent.events", sourceKey, stored); err != nil {
						return err
					}
				} else {
					stored = source
					stored.DeliveryMode = event.Interrupt
				}
				job, err := persistence.Load[scheduler.Job](ctx, e.store, "agent.jobs", sourceKey)
				if err != nil && !persistence.Missing(err) {
					return err
				}
				if err == nil && job.State == scheduler.Queued {
					job.Event = stored
					job.Version++
					if err := event.SaveJob(ctx, tx, job); err != nil {
						return err
					}
				}
				eligible = true
				bytes += len(m.Content.Text)
				if !slices.Contains(record.MessageIDs, m.ID) {
					record.MessageIDs = append(record.MessageIDs, m.ID)
					changed = true
				}
				if len(record.MessageIDs) > 200 || bytes > 512<<10 {
					return core.Invalid("expedite", "too many unread messages for one context")
				}
			}
			if page.NextCursor == "" || (len(page.Items) > 0 && page.Items[len(page.Items)-1].Sequence >= r.ThroughSequence) {
				break
			}
			q.Page.Cursor = page.NextCursor
		}
		if !eligible {
			result.State = "not_needed"
			return tx.Put(ctx, "agent.expedite_receipts", receiptKey, expediteReceipt{State: result.State, ConversationID: r.ConversationID, ThroughSequence: r.ThroughSequence})
		}
		if err := tx.Put(ctx, expediteBucket, key, record); err != nil {
			return err
		}
		if err := tx.NotifyAfterCommit(ctx, event.AssistantInputSignal(p.OwnerID, id)); err != nil {
			return err
		}
		if err := tx.Put(ctx, "agent.expedite_receipts", receiptKey, expediteReceipt{ConversationID: r.ConversationID, ThroughSequence: r.ThroughSequence}); err != nil {
			return err
		}
		jobs, err := persistence.Select[scheduler.Job](ctx, e.store, "agent.jobs", persistence.Query{Where: []persistence.Filter{persistence.Eq("Event.OwnerID", p.OwnerID), persistence.Eq("Event.AssistantID", id), persistence.Eq("ExecutionID", status.ExecutionID), persistence.Eq("State", scheduler.Retry)}})
		if err != nil {
			return err
		}
		for _, job := range jobs {
			if job.Event.OwnerID == p.OwnerID && job.Event.AssistantID == id && job.ExecutionID == status.ExecutionID && job.State == scheduler.Retry {
				job.NotBefore = e.options.Now()
				job.Version++
				if err := event.SaveJob(ctx, tx, job); err != nil {
					return err
				}
			}
		}
		return e.expediteVisible(ctx, p.OwnerID, id)
	})
	if err != nil {
		return ExpediteStatus{}, err
	}
	if changed {
		e.mu.Lock()
		active := e.active[key]
		if active.modelCancel != nil && active.id == expeditedExecution {
			active.modelCancel(errExpedited)
		}
		e.mu.Unlock()
	}
	if result.State == "not_running" || result.State == "not_needed" {
		return result, nil
	}
	return e.expediteStatus(ctx, p, id)
}
func (e *Engine) expediteVisible(ctx context.Context, owner, id core.ID) error {
	return realtime.NewStore(e.store).Append(ctx, realtime.Visible{ID: stableID(string(owner), string(id), time.Now().String()), OwnerID: owner, Topic: "assistant.expedite", Object: core.Object{Kind: "assistant", ID: id}, CreatedAt: e.options.Now().UTC()})
}
func (e *Engine) expediteStatus(ctx context.Context, p core.Principal, id core.ID) (ExpediteStatus, error) {
	record, err := persistence.Load[expediteRecord](ctx, e.store, expediteBucket, persistence.Key(p.OwnerID, id))
	if persistence.Missing(err) {
		return ExpediteStatus{}, nil
	}
	if err != nil {
		return ExpediteStatus{}, err
	}
	result := ExpediteStatus{ThroughSequence: record.ThroughSequence, State: "read"}
	for _, mid := range record.MessageIDs {
		page, err := e.options.Conversations.Messages(ctx, p, conversation.MessageQuery{ConversationID: record.ConversationID, MessageID: mid})
		if err != nil {
			return ExpediteStatus{}, err
		}
		if len(page.Items) > 0 && page.Items[0].ReadAt == nil {
			result.State = "pending"
			break
		}
	}
	if result.State == "pending" {
		e.mu.Lock()
		active, ok := e.active[persistence.Key(p.OwnerID, id)]
		e.mu.Unlock()
		if !ok {
			result.State = "waiting"
		} else if active.phase == "tool" {
			result.State = "waiting_tool"
		}
	}
	return result, nil
}
func (e *Engine) pendingExpedite(ctx context.Context, t Turn, joined []core.ID) ([]core.ID, error) {
	if t.Event.Kind == event.DreamDue {
		return nil, nil
	}
	record, err := persistence.Load[expediteRecord](ctx, e.store, expediteBucket, persistence.Key(t.Principal.OwnerID, t.Event.AssistantID))
	if persistence.Missing(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []core.ID
	for _, id := range record.MessageIDs {
		if !slices.Contains(joined, id) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// intakeExpedite uses the existing authorized conversation tool to obtain exact
// messages and attachment metadata. User text never becomes a system instruction.
func (e *Engine) intakeExpedite(ctx context.Context, t Turn, p *turnProgress) error {
	ids, err := e.pendingExpedite(ctx, t, p.Joined)
	if err != nil || len(ids) == 0 {
		return err
	}
	record, err := persistence.Load[expediteRecord](ctx, e.store, expediteBucket, persistence.Key(t.Principal.OwnerID, t.Event.AssistantID))
	if err != nil {
		return err
	}
	next := *p
	next.Finished = false
	next.Inputs = append([]model.Input{}, p.Inputs...)
	next.Joined = append([]core.ID{}, p.Joined...)
	next.Inputs = append(next.Inputs, model.Input{Role: model.User, Text: prompt.Text("expedite", nil)})
	var messages []conversation.Message
	for _, id := range ids {
		args, _ := json.Marshal(map[string]any{"conversation_id": record.ConversationID, "message_id": id})
		execution := core.Execution{Principal: t.Principal, AssistantID: t.Event.AssistantID, TaskID: t.Event.TaskID, OperationID: stableID(string(t.Event.ID), "expedite", string(id))}
		result, err := e.options.Dependencies.Tools.Execute(ctx, tools.Call{Execution: execution, Name: "conversation_read", Arguments: args})
		if err != nil {
			return err
		}
		if result.Failure != nil {
			return core.NewError(result.Failure.Code, "expedited message read failed")
		}
		var page core.Page[conversation.Message]
		if json.Unmarshal(result.Content, &page) != nil || len(page.Items) != 1 || page.Items[0].ID != id || page.Items[0].Sender != conversation.User {
			return core.NewError(core.FailedPrecondition, "expedited message unavailable")
		}
		next.Inputs = append(next.Inputs, model.Input{Role: model.User, Text: toolText("conversation_read", string(result.Content)), ReadMessageIDs: []core.ID{id}})
		next.Joined = append(next.Joined, id)
		messages = append(messages, page.Items[0])
	}
	if err := checkContextSize(next.Inputs); err != nil {
		return err
	}
	err = e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := e.authorize(ctx, t.Principal, t.Event.AssistantID, policy.Execute); err != nil {
			return err
		}
		for _, m := range messages {
			if err := e.options.Conversations.AcceptUserMessage(ctx, core.Execution{Principal: t.Principal, AssistantID: t.Event.AssistantID, TaskID: t.Event.TaskID, OperationID: t.Event.ID}, m.ID); err != nil {
				return err
			}
			if m.ID == t.Event.MessageID {
				continue
			}
			source := scheduler.UserMessageEvent(t.Principal.OwnerID, t.Event.AssistantID, m)
			source.DeliveryMode = event.Interrupt
			if err := event.NewWriter(e.store).AppendAssistant(ctx, source); err != nil {
				return err
			}
			if err := tx.Put(ctx, "agent.events", persistence.Key(t.Principal.OwnerID, source.ID), source); err != nil {
				return err
			}
			key := persistence.Key(t.Principal.OwnerID, source.ID)
			job, err := persistence.Load[scheduler.Job](ctx, e.store, "agent.jobs", key)
			if persistence.Missing(err) {
				job = scheduler.Job{Event: source, State: scheduler.Queued, NotBefore: source.CreatedAt, Version: 1}
			} else if err != nil {
				return err
			}
			if job.State == scheduler.Done || job.State == scheduler.Canceled {
				continue
			}
			if job.State == scheduler.Running || (job.JoinedTo != "" && job.JoinedTo != t.Event.ID) {
				return core.NewError(core.Conflict, "message already executing")
			}
			job.JoinedTo = t.Event.ID
			job.Version++
			if err := event.SaveJob(ctx, tx, job); err != nil {
				return err
			}
		}
		return e.saveProgress(ctx, t, &next)
	})
	if err == nil {
		*p = next
		e.options.Trace.Record(ctx, "conversation.expedited", "succeeded", "", map[string]any{"MessageIDs": ids})
	}
	return err
}
func (e *Engine) finishExpedite(ctx context.Context, t Turn, p turnProgress) error {
	return e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		jobs, err := persistence.Select[scheduler.Job](ctx, e.store, "agent.jobs", persistence.Query{Where: []persistence.Filter{persistence.Eq("Event.OwnerID", t.Principal.OwnerID), persistence.Eq("JoinedTo", t.Event.ID)}})
		if err != nil {
			return err
		}
		for _, job := range jobs {
			if job.Event.OwnerID != t.Principal.OwnerID || job.JoinedTo != t.Event.ID {
				continue
			}
			if job.State == scheduler.Canceled {
				continue
			}
			if slices.Contains(p.Handled, job.Event.MessageID) {
				job.State = scheduler.Done
			} else {
				job.State = scheduler.Queued
			}
			job.JoinedTo = ""
			job.ExecutionID = t.Principal.ExecutionID
			job.Version++
			if err := event.SaveJob(ctx, tx, job); err != nil {
				return err
			}
		}
		key := persistence.Key(t.Principal.OwnerID, t.Event.AssistantID)
		record, err := persistence.Load[expediteRecord](ctx, e.store, expediteBucket, key)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if err == nil {
			record.MessageIDs = slices.DeleteFunc(record.MessageIDs, func(id core.ID) bool { return slices.Contains(p.Handled, id) })
			if err := tx.Put(ctx, expediteBucket, key, record); err != nil {
				return err
			}
		}
		return tx.Delete(ctx, progressBucket, persistence.Key(t.Principal.OwnerID, t.Event.ID))
	})
}
func conversationReadIDs(call model.ToolCall, result tools.Result) []core.ID {
	if call.Name != "conversation_read" || result.Failure != nil {
		return nil
	}
	var page core.Page[conversation.Message]
	if json.Unmarshal(result.Content, &page) != nil {
		return nil
	}
	var ids []core.ID
	for _, m := range page.Items {
		if m.Sender == conversation.User {
			ids = append(ids, m.ID)
		}
	}
	return ids
}
