package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/debugtrace"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
)

func showWorkbench(t *testing.T, b *Bundle, p core.Principal, enabled bool) error {
	t.Helper()
	pref, err := b.Services.Settings.Get(t.Context(), p)
	if err != nil {
		return err
	}
	pref.WorkbenchVisible = enabled
	_, err = b.Services.Settings.Update(t.Context(), p, settings.UpdateRequest{Mutation: core.Mutation{IdempotencyKey: string(core.ID(time.Now().Format("150405.000000000"))), ExpectedVersion: pref.Version}, Settings: pref})
	return err
}
func debugRequest(b *Bundle, token core.Secret, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest("GET", "https://runtime.example"+path, nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("X-Forwarded-Proto", "https")
	request.AddCookie(&http.Cookie{Name: "__Host-molr_session", Value: token.Reveal()})
	response := httptest.NewRecorder()
	b.HTTP.Handler().ServeHTTP(response, request)
	return response
}
func TestWorkbenchDisabledByDeploymentCannotBeShownByOwner(t *testing.T) {
	b, err := Build(t.Context(), testConfig(t))
	need(t, err)
	defer b.Close()
	p, token := ownerSession(t, b)
	if err := showWorkbench(t, b, p, true); core.CodeOf(err) != core.FailedPrecondition {
		t.Fatalf("deployment boundary bypassed: %v", err)
	}
	for _, path := range []string{"/api/debug/trajectory", "/api/debug/runs", "/api/debug/stream", "/api/debug/assistants/missing/checkpoint"} {
		if r := debugRequest(b, token, path); r.Code != 403 {
			t.Fatalf("disabled route %s: %d", path, r.Code)
		}
	}
}
func TestWorkbenchCapturesActualLoopAndKeepsPrivateDataOutOfChat(t *testing.T) {
	cfg := testConfig(t)
	cfg.Debug.Enabled = true
	b, err := Build(t.Context(), cfg)
	need(t, err)
	defer b.Close()
	p, token := ownerSession(t, b)
	ctx := t.Context()
	if r := debugRequest(b, token, "/api/debug/runs"); r.Code != 200 {
		t.Fatal("hidden workbench denied reading")
	}
	var calls atomic.Int32
	var chatID atomic.Value
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		msg := map[string]any{"role": "assistant", "content": "PRIVATE_MODEL_OUTPUT"}
		finish := "stop"
		if n == 1 {
			args, _ := json.Marshal(map[string]any{"conversation_id": chatID.Load(), "limit": 5})
			msg["tool_calls"] = []any{map[string]any{"id": "read-chat", "type": "function", "function": map[string]any{"name": "conversation_read", "arguments": string(args)}}}
			finish = "tool_calls"
		}
		w.Header().Set("X-Private-Header", "HEADER_SECRET")
		json.NewEncoder(w).Encode(map[string]any{"id": "response", "api_key": "RESPONSE_SECRET", "choices": []any{map[string]any{"message": msg, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 30, "completion_tokens": 10}})
	}))
	defer provider.Close()
	binding := resource.Binding{Endpoint: provider.URL, AllowedOrigins: []string{provider.URL}, Models: []resource.ModelConfig{{Name: "test", ContextLimit: 32768, SupportsTools: true, Pricing: &resource.ModelPricing{Currency: "USD"}}}}
	r, err := b.Services.Resources.Connect(ctx, p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "provider"}, Name: "provider", Kind: resource.Model, Provider: "openai-compatible", Binding: binding})
	need(t, err)
	a, err := b.Services.Assistants.Hire(ctx, p, assistant.HireRequest{Mutation: core.Mutation{IdempotencyKey: "hire"}, Name: "assistant", ModelID: modelEntries(r, binding)[0].id, Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}})
	need(t, err)
	cancelPendingBirth(t, b, a)
	chatID.Store(a.ConversationID)
	_, err = b.Services.Conversations.SendUser(ctx, p, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "send"}, ConversationID: a.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: "HISTORY_ONLY_VIA_TOOL"}})
	need(t, err)
	pending, err := conversation.NewOutbox(b.store).Pending(ctx, 100)
	need(t, err)
	consumer := scheduler.NewMessageConsumer(b.store, scheduler.Options{Conversations: conversation.NewRepository(b.store)})
	for _, message := range pending {
		need(t, consumer.AcceptMessage(ctx, message))
	}
	claim, err := scheduler.NewQueue(b.store).Claim(ctx, time.Now())
	need(t, err)
	out, err := b.Services.Agent.(agent.Runtime).Handle(ctx, agent.Turn{Principal: claim.Principal, Event: claim.Job.Event, Attempt: claim.Job.Attempts})
	need(t, err)
	runs, err := b.Trace.Runs(ctx, p, debugtrace.Query{AssistantID: a.ID})
	need(t, err)
	if len(runs.Items) != 1 || runs.Items[0].State != "succeeded" {
		t.Fatalf("trace missing: %+v", runs)
	}
	run, err := b.Trace.Get(ctx, p, runs.Items[0].ID)
	need(t, err)
	if run.Source == nil || run.Source.Text != "HISTORY_ONLY_VIA_TOOL" || run.Source.Origin != "capture" {
		t.Fatalf("missing captured trigger: %+v", run.Source)
	}
	found := map[string]int{}
	var savedSequence uint64
	for _, step := range run.Steps {
		found[step.Kind]++
		payload, err := b.Trace.Detail(ctx, p, run.ID, step.Sequence)
		need(t, err)
		if strings.Contains(string(payload.Data), "RESPONSE_SECRET") || strings.Contains(string(payload.Data), "HEADER_SECRET") {
			t.Fatal("provider credential data leaked")
		}
		if step.Kind == "checkpoint.saved" || step.Kind == "context.build" {
			var snapshot struct {
				Checkpoint       agent.Checkpoint
				Sequence         uint64
				ContextOmitted   bool
				ContextBytes     int
				PreviousSequence uint64
				Cause            string
				Exists           bool
			}
			need(t, json.Unmarshal(payload.Data, &snapshot))
			if step.Kind == "context.build" {
				if snapshot.Exists || snapshot.Checkpoint.Sequence != 0 || strings.Contains(string(snapshot.Checkpoint.Context), string(claim.Job.Event.ID)) {
					t.Fatalf("load snapshot includes newly prepared event: %s", payload.Data)
				}
			} else {
				if snapshot.Sequence != savedSequence+1 || snapshot.PreviousSequence != savedSequence || snapshot.Cause == "" || !snapshot.ContextOmitted || snapshot.ContextBytes == 0 || len(snapshot.Checkpoint.Context) != 0 {
					t.Fatalf("invalid save snapshot: %s", payload.Data)
				}
				savedSequence++
				if savedSequence == out.Checkpoint.Sequence {
					latest, err := b.Trace.Checkpoint(ctx, p, a.ID)
					need(t, err)
					var committed agent.Checkpoint
					need(t, json.Unmarshal(latest.Data, &committed))
					if committed.Sequence != snapshot.Sequence || len(committed.Context) == 0 {
						t.Fatal("latest recovery checkpoint missing")
					}
				}
			}
		}
		if step.Kind == "model.request" && found[step.Kind] == 1 && strings.Contains(string(payload.Data), "HISTORY_ONLY_VIA_TOOL") {
			t.Fatal("trace invented implicit chat context")
		}
		if step.Kind == "model.request" && found[step.Kind] == 2 && !strings.Contains(string(payload.Data), "HISTORY_ONLY_VIA_TOOL") {
			t.Fatal("trace lost tool result from actual request")
		}
		if step.Kind == "model.request" && found[step.Kind] == 3 && !strings.Contains(string(payload.Data), "Did you forget to send a message?") {
			t.Fatal("trace lost the one-time publication reminder")
		}
	}
	if found["model.request"] != 3 || found["model.response"] != 3 || found["tool.complete"] != 1 || found["checkpoint.saved"] != int(out.Checkpoint.Sequence) || savedSequence < 3 || found["context.build"] != 1 || found["budget.decision"] != 3 {
		t.Fatalf("missing stages: %+v", found)
	}
	need(t, showWorkbench(t, b, p, true))
	response := debugRequest(b, token, "/api/debug/runs/"+string(run.ID))
	if response.Code != 200 {
		t.Fatal("trace HTTP read denied", response.Code)
	}
	trajectoryResponse := debugRequest(b, token, "/api/debug/trajectory?assistant_id="+string(a.ID)+"&limit=2")
	if trajectoryResponse.Code != 200 {
		t.Fatal("trajectory HTTP read denied", trajectoryResponse.Code)
	}
	var trajectory debugtrace.TrajectoryPage
	need(t, json.Unmarshal(trajectoryResponse.Body.Bytes(), &trajectory))
	if len(trajectory.Items) != 2 || len(trajectory.Runs) != 1 || trajectory.NextCursor == "" || trajectory.HeadCursor == "" {
		t.Fatalf("invalid trajectory response: %+v", trajectory)
	}
	if debugRequest(b, token, "/api/debug/trajectory?limit=201").Code != 400 {
		t.Fatal("unbounded trajectory page accepted")
	}
	messages, err := b.Services.Conversations.Messages(ctx, p, conversation.MessageQuery{ConversationID: a.ConversationID})
	need(t, err)
	encoded, _ := json.Marshal(messages)
	if strings.Contains(string(encoded), "PRIVATE_MODEL_OUTPUT") {
		t.Fatal("private model output leaked to chat")
	}
	streamCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	request := httptest.NewRequest("GET", "https://runtime.example/api/debug/stream", nil).WithContext(streamCtx)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("X-Forwarded-Proto", "https")
	request.AddCookie(&http.Cookie{Name: "__Host-molr_session", Value: token.Reveal()})
	streamed := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { b.HTTP.Handler().ServeHTTP(streamed, request); close(done) }()
	time.AfterFunc(200*time.Millisecond, func() { _ = showWorkbench(t, b, p, false) })
	<-done
	body, _ := io.ReadAll(streamed.Result().Body)
	if !strings.Contains(string(body), "event: change") || strings.Contains(string(body), "event: disabled") || strings.Contains(string(body), "PRIVATE_MODEL_OUTPUT") {
		t.Fatalf("unsafe or visibility-gated stream: %s", body)
	}
	if debugRequest(b, token, "/api/debug/trajectory").Code != 200 || debugRequest(b, token, "/api/debug/runs/"+string(run.ID)).Code != 200 {
		t.Fatal("hidden trace became unreadable")
	}
}

func TestWorkbenchBrokenRecordingDoesNotBlockRuntimeStartup(t *testing.T) {
	cfg := testConfig(t)
	cfg.Debug.Enabled = true
	b, err := Build(t.Context(), cfg)
	need(t, err)
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "debug.summaries", "owner/broken", "invalid summary shape")
	}))
	need(t, b.Close())
	b, err = Build(t.Context(), cfg)
	need(t, err)
	defer b.Close()
	ownerSession(t, b)
}
