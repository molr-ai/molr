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
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/modelcatalog"
	"molr.ai/runtime/internal/persistence"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDreamRealModelFormationAndFileSearch(t *testing.T) {
	ctx := t.Context()
	b, err := Build(ctx, testConfig(t))
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	var mu sync.Mutex
	var requests []string
	sourceID := "yesterday-message"
	memoryPath := ""
	chatID := ""
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct{ Role, Content string }
			Tools    []json.RawMessage
		}
		json.Unmarshal(raw, &req)
		mu.Lock()
		requests = append(requests, string(raw))
		mu.Unlock()
		msg := map[string]any{"role": "assistant", "content": "done"}
		finish := "stop"
		if strings.Contains(string(raw), "private dream memory consolidation") {
			if len(req.Messages) < 2 || req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
				t.Error("dream requires stable instructions and evidence")
			}
			if len(req.Tools) != 7 {
				t.Errorf("dream file tools: %d", len(req.Tools))
			}
			if len(req.Messages) == 2 {
				calls := []any{}
				for i, item := range []struct{ path, text string }{
					{".agents/memory/用户偏好/草稿.md", "项目周报先写结论"},
					{"MEMORY.md", "工作汇报先说明结论，再提供依据。"},
				} {
					args, _ := json.Marshal(map[string]any{"path": item.path, "text": item.text, "expected_version": "", "overwrite": false, "create_parents": true})
					calls = append(calls, map[string]any{"id": fmt.Sprintf("dream-write-%d", i), "type": "function", "function": map[string]any{"name": "file_write", "arguments": string(args)}})
				}
				msg["tool_calls"], finish = calls, "tool_calls"
			} else if len(req.Messages) == 5 {
				var saved struct{ Version string }
				if err := json.Unmarshal([]byte(req.Messages[3].Content), &saved); err != nil || saved.Version == "" {
					t.Error("Dream mutation receipt lost on the actual model wire", req.Messages[3].Content)
					http.Error(w, "missing receipt", 500)
					return
				}
				args, _ := json.Marshal(map[string]any{"path": ".agents/memory/用户偏好/草稿.md", "to_path": ".agents/memory/用户偏好/项目周报.md", "expected_version": saved.Version, "create_parents": true})
				msg["tool_calls"] = []any{map[string]any{"id": "dream-move", "type": "function", "function": map[string]any{"name": "file_move", "arguments": string(args)}}}
				finish = "tool_calls"
			} else {
				msg["content"] = "已创建周报主题并整理核心认知。"
			}

		} else {
			count := 0
			for _, v := range req.Messages {
				if v.Role == "tool" {
					count++
				}
			}
			name := ""
			var args any
			switch count {
			case 0:
				name = "conversation_read"
				args = map[string]any{"conversation_id": chatID, "limit": 1}
			case 1:
				name = "file_search"
				args = map[string]any{"path": "/work/.agents/memory", "pattern": "项目周报", "glob": []string{"**/*.md"}}
			case 2:
				name = "file_read"
				args = map[string]any{"path": memoryPath}
			case 3:
				name = "conversation_publish"
				args = map[string]any{"conversation_id": chatID, "text": "我会按记忆中的偏好，项目周报先写结论。"}
			}
			if name != "" {
				data, _ := json.Marshal(args)
				msg["tool_calls"] = []any{map[string]any{"id": name, "type": "function", "function": map[string]any{"name": name, "arguments": string(data)}}}
				finish = "tool_calls"
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "receipt", "choices": []any{map[string]any{"message": msg, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 100}})
	}))
	defer provider.Close()
	r, err := b.Services.Resources.Connect(ctx, p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "provider"}, Kind: resource.Model, Name: "test dream model", Provider: "openai-compatible", Binding: resource.Binding{Endpoint: provider.URL, AllowedOrigins: []string{provider.URL}, Models: []resource.ModelConfig{{Name: "dream-test", ContextLimit: 32768, SupportsTools: true, Pricing: &resource.ModelPricing{Currency: "USD"}}}}})
	need(t, err)
	a, err := b.Services.Assistants.Hire(ctx, p, assistant.HireRequest{Mutation: core.Mutation{IdempotencyKey: "hire"}, Name: "Dreamer", ModelID: modelcatalog.Entries(r, resource.Binding{Models: []resource.ModelConfig{{Name: "dream-test"}}})[0].ID, Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}})
	need(t, err)
	cancelPendingBirth(t, b, a)
	chatID = string(a.ConversationID)
	cfg, err := b.Services.Dreams.Get(ctx, p, a.ID)
	need(t, err)
	loc, _ := time.LoadLocation(cfg.Timezone)
	n := time.Now().In(loc)
	yesterday := time.Date(n.Year(), n.Month(), n.Day()-1, 12, 0, 0, 0, loc)
	_, err = conversation.NewRepository(b.store).Append(ctx, p.OwnerID, conversation.Message{ID: core.ID(sourceID), ConversationID: a.ConversationID, Sender: conversation.User, SenderID: p.OwnerID, Content: conversation.Content{Kind: conversation.Text, Text: "以后项目周报先写结论"}, CreatedAt: yesterday})
	need(t, err)
	run, err := b.Services.Dreams.Request(ctx, p, dream.Request{Mutation: core.Mutation{IdempotencyKey: "dream"}, AssistantID: a.ID})
	need(t, err)
	worker := b.Services.Jobs.(scheduler.Service)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- worker.Run(runCtx) }()
	defer func() { cancel(); <-done }()
	wait := func(check func() bool) {
		t.Helper()
		deadline := time.Now().Add(12 * time.Second)
		for time.Now().Before(deadline) {
			if check() {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("timed out")
	}
	wait(func() bool {
		runs, err := b.Services.Dreams.Runs(ctx, p, a.ID)
		need(t, err)
		for _, v := range runs {
			if v.ID == run.ID && v.State == "failed" {
				t.Fatal("dream failed", v.FailureCode)
			}
			if v.ID == run.ID && v.State == "completed" {
				return true
			}
		}
		return false
	})
	plans, err := b.Services.Plans.List(ctx, p, time.Now().AddDate(0, 0, -2), time.Now().AddDate(0, 0, 2))
	need(t, err)
	if len(plans) != 0 {
		t.Fatal("dream created a visible user plan", plans)
	}
	_, err = agent.NewCheckpoints(b.store).Load(ctx, p.OwnerID, a.ID)
	if !persistence.Missing(err) {
		t.Fatal("dream overwrote ordinary checkpoint", err)
	}
	page, err := b.Services.Memories.List(ctx, p, memory.Query{AssistantID: a.ID, Text: "项目周报先写结论"})
	need(t, err)
	if len(page.Items) != 1 || page.Items[0].Path != ".agents/memory/用户偏好/项目周报.md" {
		t.Fatal(page)
	}
	memoryPath = page.Items[0].Ref.LogicalPath()
	_, err = b.Services.Conversations.SendUser(ctx, p, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "today"}, ConversationID: a.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: "帮我准备项目周报"}})
	need(t, err)
	wait(func() bool {
		messages, err := b.Services.Conversations.Messages(ctx, p, conversation.MessageQuery{ConversationID: a.ConversationID})
		need(t, err)
		for _, m := range messages.Items {
			if m.Sender == conversation.Assistant {
				return true
			}
		}
		return false
	})
	wait(func() bool {
		status, err := b.Services.Agent.Status(ctx, p, a.ID)
		need(t, err)
		return status.State == agent.Idle
	})
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 8 {
		t.Fatalf("unexpected model calls: %d", len(requests))
	}
	var dreamWire, normalWire struct {
		Tools []struct {
			Function struct {
				Name       string
				Parameters any
			}
		}
	}
	need(t, json.Unmarshal([]byte(requests[0]), &dreamWire))
	need(t, json.Unmarshal([]byte(requests[3]), &normalWire))
	schemas := map[string]any{}
	for _, tool := range normalWire.Tools {
		schemas[tool.Function.Name] = tool.Function.Parameters
	}
	for _, tool := range dreamWire.Tools {
		if !reflect.DeepEqual(schemas[tool.Function.Name], tool.Function.Parameters) {
			t.Fatal("Dream duplicated or altered a common tool schema", tool.Function.Name)
		}
	}

	var normalSystem string
	for _, raw := range requests[3:] {
		var req struct {
			Messages []struct{ Role, Content string }
		}
		need(t, json.Unmarshal([]byte(raw), &req))
		if len(req.Messages) == 0 || req.Messages[0].Role != "system" {
			t.Fatal("normal execution lost its base system instruction")
		}
		if normalSystem == "" {
			normalSystem = req.Messages[0].Content
		} else if req.Messages[0].Content != normalSystem {
			t.Fatal("normal system instruction changed between tool steps")
		}
	}
	if !strings.Contains(requests[3], "工作汇报先说明结论，再提供依据。") {
		t.Fatal("dream core cognition did not enter subsequent conversation")
	}
	if strings.Contains(requests[3], "项目周报先写结论") || !strings.Contains(requests[3], "file_search") {
		t.Fatal("details were implicitly recalled or file search missing")
	}
	if strings.Contains(requests[3], "private dream memory consolidation") {
		t.Fatal("dream polluted normal execution")
	}
	if !strings.Contains(requests[6], "项目周报先写结论") {
		t.Fatal("file tool did not return memory content")
	}
}
