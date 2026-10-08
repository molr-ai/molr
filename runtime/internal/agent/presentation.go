package agent

import (
	"encoding/json"
	"strings"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/prompt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
)

// Presentation is separate from checkpoint/receipt data, which remains intact
// for skill activation, delivery evidence, reconciliation and replay.
func modelInputs(inputs []model.Input) []model.Input {
	out := append([]model.Input(nil), inputs...)
	calls := map[string]string{}
	for i, in := range out {
		for _, call := range in.Calls {
			calls[call.ID] = call.Name
		}

		if in.RuntimeContext && strings.HasPrefix(in.Text, "Runtime context_changed: ") {
			var legacy automaticContext
			if json.NewDecoder(strings.NewReader(strings.TrimPrefix(in.Text, "Runtime context_changed: "))).Decode(&legacy) == nil && legacy.Kind == "context_changed" {
				out[i].Text = contextText(&legacy)
			}
		}
		if in.Role == model.System && strings.HasPrefix(in.Text, "Runtime control event: {") {
			var legacy event.AssistantEvent
			if json.NewDecoder(strings.NewReader(strings.TrimPrefix(in.Text, "Runtime control event: "))).Decode(&legacy) == nil {
				out[i].Text = prompt.Text("control", eventPayload(legacy))
			}
		}
		if in.Role == model.Tool {
			out[i].Text = toolText(calls[in.ReplyTo], in.Text)
		}
		// Old checkpoint events may still contain serialized private fields.
		// Re-render the known envelope without altering user-authored messages.
		if in.Role == model.System && strings.HasPrefix(in.Text, "Runtime event: ") {
			var legacy struct {
				Event          event.AssistantEvent
				ConversationID core.ID `json:"conversation_id"`
			}
			if json.NewDecoder(strings.NewReader(strings.TrimPrefix(in.Text, "Runtime event: "))).Decode(&legacy) == nil && legacy.Event.ID != "" && legacy.Event.Kind != "" {
				// Legacy checkpoints stored triggering queries as system messages.
				// Project them as user turns without rewriting recovery evidence.
				out[i].Role = model.User
				out[i].Text = eventText(legacy.Event, legacy.ConversationID)
			}
		}
		if in.Role == model.User && len(in.ReadMessageIDs) > 0 && strings.HasPrefix(strings.TrimSpace(in.Text), "{") {
			var delivered struct{ Items json.RawMessage }
			if json.Unmarshal([]byte(in.Text), &delivered) == nil && len(delivered.Items) > 0 {
				out[i].Text = toolText("conversation_read", in.Text)
			}
		}

	}
	return runtimeNotificationRoles(out)
}

// Migrate only known Runtime notifications. Preserve custom system instructions,
// message contents and trusted recovery/delivery metadata in stored checkpoints.
func runtimeNotificationRoles(inputs []model.Input) []model.Input {
	out := append([]model.Input(nil), inputs...)
	for i, in := range out {
		if in.Role != model.System {
			continue
		}
		notification := in.RuntimeContext || in.Text == prompt.Text("expedite", nil)
		for _, prefix := range []string{"Runtime recovery: ", "Runtime reconciled earlier operation ", "Runtime execution handoff: ", "Runtime control event: "} {
			notification = notification || strings.HasPrefix(in.Text, prefix)
		}
		if notification {
			out[i].Role = model.User
		}
	}
	return out
}
func toolText(name, raw string) string {
	var result struct {
		Error          json.RawMessage `json:"error"`
		FullResultPath string          `json:"full_result_path"`
		Data           json.RawMessage `json:"data"`
		Provider       string          `json:"provider"`
		Usage          json.RawMessage `json:"usage"`
		DurationMS     int64           `json:"duration_ms"`
		Source         string
		Digest         string
		Text           *string
	}
	if json.Unmarshal([]byte(raw), &result) == nil && len(result.Error) > 0 {
		name = "failure"
	}
	if result.FullResultPath != "" {
		name = "archive"
	}
	if strings.HasPrefix(name, "plg_") {
		// HTTP payloads were already projected by the fixed plugin executor.
		// Keep only the public envelope; connection and ownership metadata stay
		// in the durable receipt. The JSON template is idempotent on replay.
		if len(raw) <= 16<<10 && len(result.Data) > 0 && result.Provider != "" {
			usage := "null"
			if len(result.Usage) > 0 {
				usage = string(result.Usage)
			}
			return prompt.Text("plugin-http", struct {
				Provider, Data, Usage string
				DurationMS            int64
			}{prompt.String(result.Provider), string(result.Data), usage, result.DurationMS})
		}
		if result.Text != nil && result.Source != "" && result.Digest != "" {
			return prompt.Text("plugin-skill", struct{ Source, Digest, Text string }{prompt.String(result.Source), prompt.String(result.Digest), prompt.String(*result.Text)})
		}
		name = "shell_execute"
	}
	text, err := prompt.Structured(name, raw)
	if err != nil {
		return prompt.Text("tool-unavailable", nil)
	}
	return text
}
func eventPayload(e event.AssistantEvent) string {
	// Explicit DTO: ownership, sequence/CAS and delivery internals are not prose.
	view := struct {
		ID                    core.ID
		Kind                  event.Kind
		MessageID, ScheduleID core.ID
		ScheduleVersion       core.Version `json:",omitempty"`
		OccurrenceAt          string
		OperationID, TaskID   core.ID
		CreatedAt             string
		ExecutionID           core.ID
		Details               json.RawMessage `json:",omitempty"`
	}{ID: e.ID, Kind: e.Kind, MessageID: e.MessageID, ScheduleID: e.ScheduleID, ScheduleVersion: e.ScheduleVersion, OperationID: e.OperationID, TaskID: e.TaskID, ExecutionID: e.ExecutionID, Details: e.Details}
	if !e.CreatedAt.IsZero() {
		view.CreatedAt = e.CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00")
	}
	if !e.OccurrenceAt.IsZero() {
		view.OccurrenceAt = e.OccurrenceAt.Format("2006-01-02T15:04:05.999999999Z07:00")
	}
	raw, _ := json.Marshal(view)
	body, err := prompt.Structured("event", string(raw))
	if err != nil {
		body = `{"Kind":"unavailable"}`
	}
	return body
}
func eventText(e event.AssistantEvent, conversation core.ID) string {
	body := eventPayload(e)
	return prompt.Text("event", struct {
		Event, Conversation      string
		Shell, UserMessage, Born bool
	}{body, string(conversation), e.Kind == event.ShellTaskFinished || e.Kind == event.ShellTasksRunning, e.Kind == event.UserMessage, e.Kind == event.Born})
}
func contextText(snapshot *automaticContext) string {
	view := struct {
		Sources                                       []string
		Removed                                       []contextReference
		Preferences, Permissions, Requests, Workspace bool
	}{}
	for _, entry := range snapshot.Sources {
		d := entry.ContextDocument
		if d.Kind == "tool" {
			continue
		}
		if d.Kind == "memory" {
			// Preserve the legacy memory entry, including its original encoded content.
			raw, _ := json.Marshal(entry)
			view.Sources = append(view.Sources, string(raw))
			continue
		}
		content := ""
		switch d.Kind {
		case "project_instruction", "chat_channel":
			content = prompt.String(d.Content)
		case "skill":
			if d.Loaded {
				content = prompt.String(d.Content)
			} else {
				content, _ = prompt.Structured("skill", d.Content)
			}
		case "workspace", "profile", "permissions", "permission_requests", "user_preferences":
			content, _ = prompt.Structured(d.Kind, d.Content)
		default:
			continue // obsolete source kinds are removed by refreshContext
		}
		if d.Kind == "permission_requests" && content == "[]" {
			continue
		}
		if content == "" {
			content = prompt.String("Source unavailable; do not infer permissions or reuse earlier content.")
		}
		view.Workspace = view.Workspace || d.Kind == "workspace"
		view.Preferences = view.Preferences || d.Kind == "user_preferences"
		view.Permissions = view.Permissions || d.Kind == "permissions"
		view.Requests = view.Requests || d.Kind == "permission_requests"
		view.Sources = append(view.Sources, prompt.Text("source", struct {
			Kind, Source, Scope, Content string
			Skill, Loaded                bool
		}{d.Kind, d.Source, d.Scope, content, d.Kind == "skill", d.Loaded}))
	}
	for _, ref := range snapshot.Removed {
		if ref.Kind != "tool" {
			view.Removed = append(view.Removed, ref)
		}
	}
	return prompt.Text("context", view)
}
func validateContextPresentation(d ContextDocument) error {
	if d.Kind == "workspace" || d.Kind == "profile" || d.Kind == "user_preferences" || d.Kind == "permissions" || d.Kind == "permission_requests" || d.Kind == "skill" && !d.Loaded {
		if _, err := prompt.Structured(d.Kind, d.Content); err != nil {
			return core.Invalid("context", "invalid structured context source: "+d.Kind)
		}
	}
	return nil
}

// Refresh legacy automatic text even when source revisions did not change.
func sameContextText(inputs []model.Input, snapshot *automaticContext) bool {
	expected := contextText(snapshot)
	for _, in := range inputs {
		if in.RuntimeContext {
			return strings.TrimSpace(in.Text) == expected
		}
	}
	return false
}
