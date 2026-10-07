package debugtrace

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// SourceContent describes the trigger, not the model's input. Origin distinguishes
// a captured snapshot from an owner-scoped lookup for older recordings.
type SourceContent struct {
	Kind, Title, Text, Status, Origin string
	ID                                core.ID
	Version                           core.Version
	AttachmentIDs                     []core.ID
	Truncated                         bool
}
type sourceEvent struct {
	OperationID                                 core.ID
	OwnerID, AssistantID, MessageID, ScheduleID core.ID
	Kind                                        string
	ScheduleVersion                             core.Version
}

func (s *Service) sourceContent(ctx context.Context, tx persistence.Tx, owner, assistant core.ID, event json.RawMessage, origin string) SourceContent {
	source := SourceContent{Status: "unavailable", Origin: origin}
	var ref sourceEvent
	if json.Unmarshal(event, &ref) != nil || ref.OwnerID != owner || ref.AssistantID != assistant {
		return source
	}
	source.Kind = ref.Kind
	switch ref.Kind {
	case "born":
		source.ID = assistant
		source.Title = "Born"
		source.Text = "The assistant was newly created and prompted to publish an initial message."
	case "user_message":
		if ref.MessageID.Validate() != nil {
			return source
		}
		var message struct {
			ID, ConversationID core.ID
			Sender             string
			Content            struct {
				Text          string
				AttachmentIDs []core.ID
			}
		}
		if tx.Get(ctx, "im.messages", persistence.Key(owner, ref.MessageID), &message) != nil || message.ID != ref.MessageID || message.Sender != "user" {
			return source
		}
		var conversation struct{ ID, OwnerID, PeerID core.ID }
		if tx.Get(ctx, "im.conversations", persistence.Key(owner, message.ConversationID), &conversation) != nil || conversation.ID != message.ConversationID || conversation.OwnerID != owner || conversation.PeerID != assistant {
			return source
		}
		source.ID, source.Text, source.AttachmentIDs = message.ID, message.Content.Text, message.Content.AttachmentIDs
	case "dream_due":
		var r struct {
			ID, OwnerID, AssistantID, SessionID core.ID
			Day, Timezone, State                string
			Batch                               int
			Deadline                            time.Time
			Start, End                          time.Time
			Manual, Backfill                    bool
			Sources                             []struct{ Source core.Object }
		}
		if tx.Get(ctx, "business.dream_runs", persistence.Key(owner, ref.OperationID), &r) != nil || r.OwnerID != owner || r.AssistantID != assistant {
			return source
		}
		source.ID = r.ID
		source.Title = "Dream · " + r.Day
		source.Text = fmt.Sprintf("Session: %s\nBatch: %d\nRange: %s – %s\nTimezone: %s\nDeadline: %s\nManual: %t\nBackfill: %t\nSources: %d", r.SessionID, r.Batch, r.Start.Format(time.RFC3339), r.End.Format(time.RFC3339), r.Timezone, r.Deadline.Format(time.RFC3339), r.Manual, r.Backfill, len(r.Sources))
	case "schedule_due":
		if ref.ScheduleID.Validate() != nil {
			return source
		}
		var plan struct {
			ID, OwnerID, AssistantID  core.ID
			Kind, Title, Instructions string
			Version                   core.Version
		}
		if tx.Get(ctx, "business.schedules", persistence.Key(owner, ref.ScheduleID), &plan) != nil || plan.ID != ref.ScheduleID || plan.OwnerID != owner || plan.AssistantID != assistant || plan.Kind != "assistant_plan" {
			return source
		}
		if (ref.ScheduleVersion == 0 && origin == "reference") || (ref.ScheduleVersion != 0 && plan.Version != ref.ScheduleVersion) {
			source.Status = "version_mismatch"
			return source
		}
		source.ID, source.Title, source.Text, source.Version = plan.ID, plan.Title, plan.Instructions, plan.Version
	default:
		return source
	}
	source.Status = "captured"
	encoded, status := sanitize(source, s.options.MaxPayloadBytes)
	if status == "too_large" || status == "unavailable" {
		return SourceContent{Kind: ref.Kind, Status: status, Origin: origin}
	}
	if json.Unmarshal(encoded, &source) != nil {
		return SourceContent{Status: "unavailable", Origin: origin}
	}
	source.Status = status
	return source
}
func sourceSummary(source SourceContent) *SourceContent {
	truncate := func(value string, max int) string {
		r := []rune(value)
		if len(r) > max {
			source.Truncated = true
			return string(r[:max]) + "…"
		}
		return value
	}
	source.Title = truncate(source.Title, 128)
	source.Text = truncate(source.Text, 1000)
	return &source
}
func (s *Service) sourceForRun(ctx context.Context, tx persistence.Tx, run Run) SourceContent {
	if run.Source != nil {
		return *run.Source
	}
	var payload Payload
	if tx.Get(ctx, "debug.payloads", payloadKey(run.OwnerID, run.ID, 1), &payload) != nil {
		return SourceContent{Status: "unavailable"}
	}
	if payload.Status != "captured" && payload.Status != "redacted" {
		return SourceContent{Status: payload.Status}
	}
	var data struct {
		Source *SourceContent
		Event  json.RawMessage
	}
	if json.Unmarshal(payload.Data, &data) != nil {
		return SourceContent{Status: "unavailable"}
	}
	if data.Source != nil {
		return *data.Source
	}
	return s.sourceContent(ctx, tx, run.OwnerID, run.AssistantID, data.Event, "reference")
}
