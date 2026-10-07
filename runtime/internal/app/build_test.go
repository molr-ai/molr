package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/search"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	c := config.Defaults(t.TempDir())
	c.Network.AllowedPrivateIPs = []string{"127.0.0.1"}
	c.HTTP.PublicURL = "https://runtime.example"
	c.HTTP.TLSMode = config.TrustedProxy
	c.HTTP.TrustedProxies = []string{"127.0.0.1/32"}
	return c
}
func need(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func ownerSession(t *testing.T, b *Bundle) (core.Principal, core.Secret) {
	t.Helper()
	ctx := t.Context()
	_, err := b.Bootstrap.Initialize(ctx, auth.InitializeRequest{Username: "owner", Password: core.NewSecret("correct-horse-battery-staple-2026!")})
	need(t, err)
	login, err := b.Services.Auth.Login(ctx, auth.LoginRequest{Username: "owner", Password: core.NewSecret("correct-horse-battery-staple-2026!"), Client: auth.Client{Address: "127.0.0.1", DeviceName: "test"}})
	need(t, err)
	if login.Session == nil {
		t.Fatal("missing session")
	}
	p, err := b.Services.Auth.Authenticate(ctx, auth.Authentication{Session: login.Session.Token})
	need(t, err)
	return p, login.Session.Token
}
func TestBuildRealMessageAgentAndRestart(t *testing.T) {
	cfg := testConfig(t)
	ctx := t.Context()
	b, err := Build(ctx, cfg)
	need(t, err)
	defer func() { _ = b.Close() }()
	p, token := ownerSession(t, b)
	var mu sync.Mutex
	calls := 0
	chatID := ""
	var firstInput string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		if err != nil {
			http.Error(w, "invalid", 400)
			return
		}
		// This provider fixture scripts the user-message response separately from birth.
		if strings.Contains(string(data), "You have just been created.") && !strings.Contains(string(data), "New user messages need your attention.") {
			io.WriteString(w, `{"id":"birth","choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
			return
		}
		mu.Lock()
		calls++
		n := calls
		if n == 1 {
			firstInput = string(data)
		}
		id := chatID
		mu.Unlock()
		msg := map[string]any{"role": "assistant", "content": "PRIVATE_REASON_NOT_PUBLISHED"}
		finish := "stop"
		if n <= 2 {
			name := "conversation_read"
			args := map[string]any{"conversation_id": id, "limit": 10}
			if n == 2 {
				name = "conversation_publish"
				args = map[string]any{"conversation_id": id, "text": "已收到，助理通过工具回复。"}
			}
			encoded, _ := json.Marshal(args)
			msg["tool_calls"] = []any{map[string]any{"id": "call-" + string(rune('0'+n)), "type": "function", "function": map[string]any{"name": name, "arguments": string(encoded)}}}
			finish = "tool_calls"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "receipt-" + string(rune('0'+n)), "choices": []any{map[string]any{"message": msg, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 30, "completion_tokens": 10}})
	}))
	defer provider.Close()
	r, err := b.Services.Resources.Connect(ctx, p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "model"}, Kind: resource.Model, Name: "local model", Provider: "openai-compatible", Binding: resource.Binding{Endpoint: provider.URL, AllowedOrigins: []string{provider.URL}, Models: []resource.ModelConfig{{Name: "local-test", ContextLimit: 32768, SupportsTools: true, Pricing: &resource.ModelPricing{Currency: "USD"}}}}})
	need(t, err)
	a, err := b.Services.Assistants.Hire(ctx, p, assistant.HireRequest{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}, Mutation: core.Mutation{IdempotencyKey: "hire"}, Name: "Molr", ModelID: modelEntries(r, resource.Binding{Models: []resource.ModelConfig{{Name: "local-test"}}})[0].id})
	need(t, err)
	mu.Lock()
	chatID = string(a.ConversationID)
	mu.Unlock()
	_, err = b.Services.Conversations.SendUser(ctx, p, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "hello"}, ConversationID: a.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: "UNIQUE_USER_HISTORY_ONLY_VIA_TOOL"}})
	need(t, err)
	runCtx, cancel := context.WithCancel(ctx)
	worker := b.Services.Jobs.(scheduler.Service)
	done := make(chan error, 1)
	go func() { done <- worker.Run(runCtx) }()
	defer cancel()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		jobs, err := worker.Jobs(ctx, p, scheduler.Query{})
		need(t, err)
		complete := len(jobs.Items) == 2 && jobs.Items[0].State == scheduler.Done && jobs.Items[1].State == scheduler.Done
		if complete {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("scheduler stopped: %v", err)
		case <-deadline.C:
			t.Fatalf("agent did not finish: %#v", jobs.Items)
		case <-ticker.C:
		}
	}
	cancel()
	need(t, <-done)
	messages, err := b.Services.Conversations.Messages(ctx, p, conversation.MessageQuery{ConversationID: a.ConversationID})
	need(t, err)
	if len(messages.Items) != 2 {
		t.Fatalf("expected real user + explicit publish, got %#v", messages)
	}
	if messages.Items[1].Content.Text != "已收到，助理通过工具回复。" {
		t.Fatalf("wrong publish: %#v", messages.Items)
	}
	mu.Lock()
	initial := firstInput
	n := calls
	mu.Unlock()
	if n != 3 || strings.Contains(initial, "UNIQUE_USER_HISTORY_ONLY_VIA_TOOL") {
		t.Fatalf("raw history injected or unexpected calls: %d %s", n, initial)
	}
	result, err := b.Services.Search.Search(ctx, p, search.Query{Text: "UNIQUE_USER_HISTORY"})
	need(t, err)
	if len(result.Hits.Items) != 1 {
		t.Fatalf("message search not wired: %#v", result)
	}
	need(t, b.Close())
	b, err = Build(ctx, cfg)
	need(t, err)
	p, err = b.Services.Auth.Authenticate(ctx, auth.Authentication{Session: token})
	need(t, err)
	messages, err = b.Services.Conversations.Messages(ctx, p, conversation.MessageQuery{ConversationID: a.ConversationID})
	need(t, err)
	if len(messages.Items) != 2 {
		t.Fatal("messages did not survive restart")
	}
	// HTTP is the actual composed router, with trusted proxy headers validated.
	request := httptest.NewRequest("GET", "https://runtime.example/api/assistants", nil)
	request.RemoteAddr = "127.0.0.1:30000"
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-Forwarded-Host", "runtime.example")
	request.AddCookie(&http.Cookie{Name: "__Host-molr_session", Value: token.Reveal()})
	response := httptest.NewRecorder()
	b.HTTP.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("HTTP list: %d %s", response.Code, response.Body.String())
	}
}
func TestBuildKeysPersistAndRefuseLoss(t *testing.T) {
	c := testConfig(t)
	b, err := Build(t.Context(), c)
	need(t, err)
	ownerSession(t, b)
	need(t, b.Close())
	first, err := masterKey(c.DataDir)
	need(t, err)
	second, err := masterKey(c.DataDir)
	need(t, err)
	if string(first) != string(second) {
		t.Fatal("master key changed")
	}
	clear(first)
	clear(second)
}
