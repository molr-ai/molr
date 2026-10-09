package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/dream"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDreamDeadlineQueuesMessagesPlansAndRejectsExpedite(t *testing.T) {
	testDreamQueueRelease(t, false)
}
func TestDreamManualStopPreservesQueuedMessages(t *testing.T) { testDreamQueueRelease(t, true) }
func testDreamQueueRelease(t *testing.T, stop bool) {
	ctx := t.Context()
	b, err := Build(ctx, testConfig(t))
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	started := make(chan struct{})
	var normal, dreaming atomic.Int32
	var requestMu sync.Mutex
	var dreamSystem, dreamEvidence string
	var chatID string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct{ Role, Content string }
		}
		json.Unmarshal(raw, &req)
		msg := map[string]any{"role": "assistant", "content": "done"}
		finish := "stop"
		if strings.Contains(string(raw), "private dream memory consolidation") {
			if len(req.Messages) < 2 || req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
				t.Error("dream requires system + user messages in every batch")
				http.Error(w, "No user query found in messages.", http.StatusBadRequest)
				return
			}
			requestMu.Lock()
			if dreamSystem == "" {
				dreamSystem, dreamEvidence = req.Messages[0].Content, req.Messages[1].Content
			} else if dreamSystem != req.Messages[0].Content || (len(req.Messages) == 2 && dreamEvidence == req.Messages[1].Content) {
				t.Error("dream system must remain stable while batch evidence changes")
			}
			requestMu.Unlock()
			switch dreaming.Add(1) {
			case 1:
				args, _ := json.Marshal(map[string]any{"path": ".agents/memory/工作约定/周报格式.md", "text": "Weekly reports lead with conclusions.", "expected_version": "", "overwrite": false, "create_parents": true})
				msg["tool_calls"] = []any{map[string]any{"id": "dream-file", "type": "function", "function": map[string]any{"name": "file_write", "arguments": string(args)}}}
				finish = "tool_calls"
			case 2:
				msg["content"] = "first batch completed"
			default:
				close(started)
				select {
				case <-r.Context().Done():
					return
				case <-time.After(8 * time.Second):
				}
				msg["content"] = "late summary"
			}

		} else {
			normal.Add(1)
			eventIndex := -1
			for i, v := range req.Messages {
				if strings.HasPrefix(v.Content, "Runtime event:") {
					eventIndex = i
				}
			}
			toolSeen := false
			for _, v := range req.Messages[eventIndex+1:] {
				toolSeen = toolSeen || v.Role == "tool"
			}
			if !toolSeen && eventIndex >= 0 && strings.Contains(req.Messages[eventIndex].Content, "user_message") {
				args, _ := json.Marshal(map[string]any{"conversation_id": chatID, "limit": 1})
				msg["tool_calls"] = []any{map[string]any{"id": "read-user", "type": "function", "function": map[string]any{"name": "conversation_read", "arguments": string(args)}}}
				finish = "tool_calls"
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "receipt", "choices": []any{map[string]any{"message": msg, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 10}})
	}))
	defer provider.Close()
	resourceRow, err := b.Services.Resources.Connect(ctx, p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "provider"}, Kind: resource.Model, Name: "deadline model", Provider: "openai-compatible", Binding: resource.Binding{Endpoint: provider.URL, AllowedOrigins: []string{provider.URL}, Models: []resource.ModelConfig{{Name: "deadline-test", ContextLimit: 65536, SupportsTools: true, Pricing: &resource.ModelPricing{Currency: "USD"}}}}})
	need(t, err)
	a, err := b.Services.Assistants.Hire(ctx, p, assistant.HireRequest{Mutation: core.Mutation{IdempotencyKey: "hire"}, Name: "Dream deadline", ModelID: modelEntries(resourceRow, resource.Binding{Models: []resource.ModelConfig{{Name: "deadline-test"}}})[0].id, Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}})
	need(t, err)
	cancelPendingBirth(t, b, a)
	chatID = string(a.ConversationID)
	c, err := b.Services.Dreams.Get(ctx, p, a.ID)
	need(t, err)
	_, err = b.Services.Dreams.Configure(ctx, p, dream.ConfigureRequest{Mutation: core.Mutation{IdempotencyKey: "duration", ExpectedVersion: c.Version}, AssistantID: a.ID, Enabled: true, Hour: 3, Timezone: c.Timezone, MaxDurationSeconds: 3})
	need(t, err)
	for i := 0; i < 83; i++ {
		_, err = conversation.NewRepository(b.store).Append(ctx, p.OwnerID, conversation.Message{ID: core.ID(fmt.Sprintf("yesterday-%03d", i)), ConversationID: a.ConversationID, Sender: conversation.User, SenderID: p.OwnerID, Content: conversation.Content{Kind: conversation.Text, Text: "My weekly reports should lead with conclusions."}, CreatedAt: time.Now().Add(-24 * time.Hour)})
		need(t, err)
	}
	run, err := b.Services.Dreams.Request(ctx, p, dream.Request{Mutation: core.Mutation{IdempotencyKey: "start"}, AssistantID: a.ID})
	need(t, err)
	worker := b.Services.Jobs.(scheduler.Service)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- worker.Run(runCtx) }()
	defer func() { cancel(); <-done }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("dream did not start")
	}
	status, err := b.Services.Agent.Status(ctx, p, a.ID)
	need(t, err)
	if status.State != agent.Dreaming || status.DreamDeadline == nil {
		t.Fatal(status)
	}
	m, err := b.Services.Conversations.SendUser(ctx, p, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "message"}, ConversationID: a.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: "Please respond after dreaming."}})
	need(t, err)
	_, err = b.Services.Agent.Expedite(ctx, p, a.ID, agent.ExpediteRequest{Mutation: core.Mutation{IdempotencyKey: "expedite"}, ConversationID: a.ConversationID, ThroughSequence: m.Sequence})
	if core.CodeOf(err) != core.FailedPrecondition {
		t.Fatal("dream was expedited", err)
	}
	_, err = b.Services.Schedules.Create(ctx, p, schedule.CreateRequest{Mutation: core.Mutation{IdempotencyKey: "schedule"}, Kind: schedule.AssistantPlan, AssistantID: a.ID, Title: "queued plan", Instructions: "Wait until dreaming ends", Trigger: schedule.Trigger{StartAt: time.Now(), Timezone: "UTC", MissedPolicy: schedule.Coalesce}})
	need(t, err)
	time.Sleep(150 * time.Millisecond)
	messages, err := b.Services.Conversations.Messages(ctx, p, conversation.MessageQuery{ConversationID: a.ConversationID, MessageID: m.ID})
	need(t, err)
	if len(messages.Items) != 1 || messages.Items[0].ReadAt != nil {
		t.Fatal("queued message prematurely read", messages)
	}
	if normal.Load() != 0 {
		t.Fatal("normal execution interrupted dreaming")
	}
	if stop {
		need(t, b.Services.Agent.Stop(ctx, p, a.ID))
	}
	waitUntil := time.Now().Add(7 * time.Second)
	read := false
	for time.Now().Before(waitUntil) {
		messages, err = b.Services.Conversations.Messages(ctx, p, conversation.MessageQuery{ConversationID: a.ConversationID, MessageID: m.ID})
		need(t, err)
		if len(messages.Items) == 1 && messages.Items[0].ReadAt != nil {
			read = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !read {
		t.Fatal("message did not run after deadline")
	}
	stored, err := b.Services.Dreams.GetRun(ctx, p, run.ID)
	need(t, err)
	expected := "timed_out"
	if stop {
		expected = "canceled"
	}
	if stored.SessionState != expected || stored.State != "completed" {
		t.Fatal("timeout not recorded", stored)
	}
	mem, err := b.Services.Memories.List(ctx, p, memory.Query{AssistantID: a.ID})
	need(t, err)
	if len(mem.Items) != 1 {
		t.Fatal("committed memory lost or late result written", mem)
	}
	if dreaming.Load() != 3 {
		t.Fatal("second batch never executed", dreaming.Load())
	}
	checkpoint, err := agent.NewCheckpoints(b.store).Load(ctx, p.OwnerID, a.ID)
	if err == nil && strings.Contains(string(checkpoint.Context), "private dream") {
		t.Fatal("dream contaminated chat")
	}
	if err != nil && !persistence.Missing(err) {
		t.Fatal(err)
	}
}
