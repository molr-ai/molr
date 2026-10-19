package app

import (
	"context"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/modelcatalog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
)

func TestManualInterruptLeavesIdleAndAllLaterEventsCanRun(t *testing.T) {
	ctx := t.Context()
	waiting := make(chan core.ID, 8)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		var request struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}
		}
		if err := json.Unmarshal(data, &request); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		var input struct {
			ConversationID core.ID `json:"conversation_id"`
			Event          struct{ ID, MessageID core.ID }
		}
		eventIndex := -1
		for i, m := range request.Messages {
			if strings.HasPrefix(m.Content, "Runtime event: ") {
				// Optional fields are omitted in the model presentation; decode each event independently.
				input.Event.ID, input.Event.MessageID, input.ConversationID = "", "", ""
				_ = json.Unmarshal([]byte(strings.TrimPrefix(strings.SplitN(m.Content, "\n", 2)[0], "Runtime event: ")), &input)
				eventIndex = i
			}
		}
		if input.Event.MessageID == "" {
			input.Event.MessageID = input.Event.ID
		}
		for _, m := range request.Messages[eventIndex+1:] {
			if m.Role == "tool" {
				waiting <- input.Event.MessageID
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				select {
				case <-r.Context().Done():
				case <-ctx.Done():
				}
				return
			}
		}
		args, _ := json.Marshal(map[string]any{"conversation_id": input.ConversationID, "text": "Reply before stop: " + string(input.Event.MessageID)})
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "model-" + string(input.Event.MessageID), "choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "publish-" + string(input.Event.MessageID), "type": "function", "function": map[string]any{"name": "conversation_publish", "arguments": string(args)}}}}, "finish_reason": "tool_calls"}}, "usage": map[string]int{"prompt_tokens": 3, "completion_tokens": 1}})
	}))
	defer provider.Close()
	b, err := Build(ctx, testConfig(t))
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	modelConfig := resource.ModelConfig{Name: "stop-test", ContextLimit: 32768, SupportsTools: true, Pricing: &resource.ModelPricing{Currency: "USD"}}
	r, err := b.Services.Resources.Connect(ctx, p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "provider"}, Name: "Stop test", Kind: resource.Model, Provider: "openai-compatible", Binding: resource.Binding{Endpoint: provider.URL, AllowedOrigins: []string{provider.URL}, Models: []resource.ModelConfig{modelConfig}}})
	need(t, err)
	a, err := b.Services.Assistants.Hire(ctx, p, assistant.HireRequest{Mutation: core.Mutation{IdempotencyKey: "hire"}, Name: "Stop test", ModelID: modelcatalog.Entries(r, resource.Binding{Models: []resource.ModelConfig{modelConfig}})[0].ID, Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}})
	need(t, err)
	send := func(key string) conversation.Message {
		t.Helper()
		m, err := b.Services.Conversations.SendUser(ctx, p, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: key}, ConversationID: a.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: key}})
		need(t, err)
		return m
	}
	state := func() agent.State {
		t.Helper()
		s, err := b.Services.Agent.Status(ctx, p, a.ID)
		need(t, err)
		return s.State
	}
	// Stop also fences inputs still waiting in the IM outbox.
	stale := send("queued-before-stop")
	need(t, b.Services.Agent.Stop(ctx, p, a.ID))
	if state() != agent.Idle {
		t.Fatal("manual interrupt must leave idle")
	}
	old := send("new-input-after-first-stop")
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	worker := b.Services.Jobs.(scheduler.Service)
	go func() { done <- worker.Run(runCtx) }()
	defer func() { cancel(); need(t, <-done) }()
	await := func(want core.ID) {
		t.Helper()
		select {
		case id := <-waiting:
			if id != want {
				t.Fatalf("executed %s, expected %s", id, want)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("new input never executed")
		}
	}
	await(old.ID)
	// A visible assistant reply has committed while the turn is still active.
	page, err := b.Services.Conversations.Messages(ctx, p, conversation.MessageQuery{ConversationID: a.ConversationID})
	need(t, err)
	if !strings.Contains(page.Items[len(page.Items)-1].Content.Text, string(old.ID)) {
		t.Fatal("reply missing before stop")
	}
	need(t, b.Services.Agent.Stop(ctx, p, a.ID))
	if state() != agent.Idle {
		t.Fatal("reply/interrupt race left loop disabled")
	}
	fresh := send("new-input-immediately-after-reply-and-pause")
	await(fresh.ID)
	need(t, b.Services.Agent.Stop(ctx, p, a.ID))
	if state() != agent.Idle {
		t.Fatal("pause left loop disabled")
	}
	// Operation notifications resume the loop without a user message.
	completion := event.AssistantEvent{ID: "operation-notification", OwnerID: p.OwnerID, AssistantID: a.ID, Kind: event.OperationCompleted, OperationID: "finished-operation", CreatedAt: time.Now()}
	need(t, event.NewWriter(b.store).AppendAssistant(ctx, completion))
	await(completion.ID)
	queued := send("queued-while-other-event-runs")
	need(t, b.Services.Agent.Stop(ctx, p, a.ID))
	next := send("new-input-after-second-stop")
	await(next.ID)
	if state() != agent.Working {
		t.Fatal("late cancellation stopped the new turn")
	}
	jobs, err := worker.Jobs(ctx, p, scheduler.Query{AssistantID: a.ID})
	need(t, err)
	for _, job := range jobs.Items {
		if (job.Event.MessageID == stale.ID || job.Event.MessageID == queued.ID || job.Event.MessageID == old.ID || job.Event.MessageID == fresh.ID) && job.State != scheduler.Canceled {
			t.Fatalf("stopped input revived: %+v", job)
		}
		if job.Event.MessageID == next.ID && job.State != scheduler.Running {
			t.Fatalf("new input not running: %+v", job)
		}
	}
	need(t, b.Services.Agent.Stop(ctx, p, a.ID))
	if state() != agent.Idle {
		t.Fatal("loop did not return to idle")
	}
	events, err := persistence.Scan[event.AssistantEvent](ctx, b.store, "agent.events")
	need(t, err)
	controls := 0
	for _, input := range events {
		if input.Kind == event.UserStopped {
			controls++
		}
	}
	if controls != 5 {
		t.Fatalf("manual control events=%d", controls)
	}
	_, err = scheduler.NewQueue(b.store).Claim(ctx, time.Now().Add(time.Hour))
	if core.CodeOf(err) != core.NotFound {
		t.Fatalf("control event started another turn: %v", err)
	}
}
