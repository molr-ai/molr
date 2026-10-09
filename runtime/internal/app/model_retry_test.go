package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/debugtrace"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/quota"
)

func retryFixture(t *testing.T, handler http.HandlerFunc) (*Bundle, core.Principal, assistant.Assistant, config.Config) {
	t.Helper()
	cfg := testConfig(t)
	cfg.Debug.Enabled = true
	cfg.Execution.ModelRetry.BaseDelay = config.Duration(2 * time.Millisecond)
	cfg.Execution.ModelRetry.MaxDelay = config.Duration(4 * time.Millisecond)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	b, err := Build(t.Context(), cfg)
	need(t, err)
	t.Cleanup(func() { b.Close() })
	p, _ := ownerSession(t, b)
	need(t, showWorkbench(t, b, p, true))
	binding := resource.Binding{Endpoint: server.URL, AllowedOrigins: []string{server.URL}, Models: []resource.ModelConfig{{Name: "retry-model", ContextLimit: 32768, SupportsTools: true, Pricing: &resource.ModelPricing{Currency: "USD", InputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 1000000}}}}
	r, err := b.Services.Resources.Connect(t.Context(), p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "provider"}, Name: "provider", Kind: resource.Model, Provider: "openai-compatible", Binding: binding})
	need(t, err)
	a, err := b.Services.Assistants.Hire(t.Context(), p, assistant.HireRequest{Mutation: core.Mutation{IdempotencyKey: "hire"}, Name: "retry assistant", ModelID: modelEntries(r, binding)[0].id, Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD", Micros: 1000000000}, Period: "daily"}})
	need(t, err)
	return b, p, a, cfg
}
func runRetryJob(t *testing.T, b *Bundle, p core.Principal, a assistant.Assistant) scheduler.Job {
	t.Helper()
	// Hiring already enqueued born; exercise normal retry behavior with that event.
	return awaitRetryJob(t, b, p, a)
}
func awaitRetryJob(t *testing.T, b *Bundle, p core.Principal, a assistant.Assistant) scheduler.Job {
	t.Helper()
	worker := b.Services.Jobs.(scheduler.Service)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	defer func() { cancel(); need(t, <-done) }()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		jobs, err := worker.Jobs(t.Context(), p, scheduler.Query{AssistantID: a.ID})
		need(t, err)
		if len(jobs.Items) == 1 && (jobs.Items[0].State == scheduler.Done || jobs.Items[0].State == scheduler.Failed || jobs.Items[0].State == scheduler.NeedsReview) {
			return jobs.Items[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return scheduler.Job{}
}
func TestModelRetriesAppearAsDistinctWorkbenchRequestsAndSettleOnce(t *testing.T) {
	var calls atomic.Int32
	var chat string
	var mu sync.Mutex
	var requests []string
	b, p, a, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests = append(requests, string(raw))
		mu.Unlock()
		n := calls.Add(1)
		if n <= 2 {
			w.WriteHeader(429)
			io.WriteString(w, `{"error":{"code":"rate_limit_exceeded"}}`)
			return
		}
		message := map[string]any{"role": "assistant", "content": "completed"}
		finish := "stop"
		if n == 3 {
			args, _ := json.Marshal(map[string]any{"conversation_id": chat, "text": "Published once after retry."})
			message["tool_calls"] = []any{map[string]any{"id": "publish-once", "type": "function", "function": map[string]any{"name": "conversation_publish", "arguments": string(args)}}}
			finish = "tool_calls"
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "response", "choices": []any{map[string]any{"message": message, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 30, "completion_tokens": 10}})
	})
	chat = string(a.ConversationID)
	job := runRetryJob(t, b, p, a)
	if job.State != scheduler.Done || calls.Load() != 4 || job.Attempts != 3 {
		t.Fatalf("job=%+v calls=%d", job, calls.Load())
	}
	mu.Lock()
	if requests[0] != requests[1] || requests[1] != requests[2] {
		t.Error("retry changed payload")
	}
	mu.Unlock()
	messages, err := b.Services.Conversations.Messages(t.Context(), p, conversation.MessageQuery{ConversationID: a.ConversationID})
	need(t, err)
	if len(messages.Items) != 1 {
		t.Fatalf("tool effects repeated: %+v", messages)
	}
	trajectory, err := b.Trace.Trajectory(t.Context(), p, debugtrace.TrajectoryQuery{Query: debugtrace.Query{AssistantID: a.ID, Page: core.PageRequest{Limit: 200}}})
	need(t, err)
	starts := map[core.ID]int{}
	logical := map[core.ID][]int{}
	waits := 0
	for _, entry := range trajectory.Items {
		step := entry.Step
		if step.Kind == "model.start" {
			starts[step.OperationID]++
			logical[step.LogicalID] = append(logical[step.LogicalID], step.Attempt)
		}
		if step.Kind == "model.retry" {
			waits++
			if step.NextRetryAt == nil || step.HTTPStatus != 429 {
				t.Fatalf("retry metadata missing: %+v", step)
			}
		}
		if step.Kind == "model.response" {
			payload, err := b.Trace.Detail(t.Context(), p, entry.RunID, step.Sequence)
			need(t, err)
			if len(payload.Data) == 0 {
				t.Fatal("response missing")
			}
		}
	}
	if len(starts) != 4 || len(logical) != 2 || waits != 2 {
		t.Fatalf("workbench lost attempts: starts=%v logical=%v waits=%d", starts, logical, waits)
	}
	for _, n := range starts {
		if n != 1 {
			t.Fatal("duplicate operation")
		}
	}
	for _, run := range trajectory.Runs {
		if run.EventID != job.Event.ID || run.ExecutionID != job.ExecutionID {
			t.Fatal("retry changed turn identity")
		}
		if run.Attempt < 3 && run.State != "retrying" {
			t.Fatalf("retry prematurely failed: %+v", run)
		}
	}
	reservations, err := persistence.Scan[struct{ Reservation struct{ State string } }](t.Context(), b.store, "security.reservations")
	need(t, err)
	counts := map[string]int{}
	for _, r := range reservations {
		counts[r.Reservation.State]++
	}
	if counts["released"] != 2 || counts["settled"] != 2 || counts["unknown"] != 0 {
		t.Fatalf("budget: %v", counts)
	}
	status, err := b.Services.Agent.Status(t.Context(), p, a.ID)
	need(t, err)
	if status.ModelRetry != nil {
		t.Fatal("stale retry banner")
	}
}
func TestModelPermanentUnknownAndExhaustedFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		calls  int
		state  scheduler.State
	}{
		{"authentication", 401, `{}`, 1, scheduler.Failed}, {"invalid", 400, `{}`, 1, scheduler.Failed},
		{"quota", 429, `{"error":{"code":"insufficient_quota"}}`, 1, scheduler.Failed},
		{"gateway", 504, `{}`, 1, scheduler.NeedsReview}, {"exhausted", 429, `{}`, 4, scheduler.Failed},
		{"long_retry_after", 429, `{}`, 1, scheduler.Failed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			b, p, a, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if tc.name == "long_retry_after" {
					w.Header().Set("Retry-After", "121")
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})
			job := runRetryJob(t, b, p, a)
			if calls.Load() != int32(tc.calls) || job.State != tc.state {
				t.Fatalf("job=%+v calls=%d", job, calls.Load())
			}
			usage, err := quota.ModelTotals(t.Context(), b.store, p.OwnerID, a.ModelID, time.Time{}, time.Time{})
			need(t, err)
			if usage.Active != 0 || len(usage.Reserved) != 0 {
				t.Fatalf("finished call retained quota: %+v", usage)
			}
			if tc.state == scheduler.NeedsReview && (usage.Unknown != 1 || usage.Calls != 1 || len(usage.Spent) != 0) {
				t.Fatalf("unknown cost was erased or charged: %+v", usage)
			}
			status, err := b.Services.Agent.Status(t.Context(), p, a.ID)
			need(t, err)
			if status.State != agent.Waiting || status.ModelRetry != nil {
				t.Fatalf("bad terminal status: %+v", status)
			}
		})
	}
}

func deferFirstRetry(t *testing.T, b *Bundle, p core.Principal, a assistant.Assistant) scheduler.Job {
	t.Helper()
	// Hiring already enqueued born; exercise normal retry behavior with that event.
	claim, err := scheduler.NewQueue(b.store).Claim(t.Context(), time.Now())
	need(t, err)
	_, err = b.Services.Agent.(agent.Runtime).Handle(t.Context(), agent.Turn{Principal: claim.Principal, Event: claim.Job.Event, Attempt: claim.Job.Attempts})
	if !scheduler.IsDeferred(err) {
		t.Fatalf("expected durable retry: %v", err)
	}
	jobs, err := b.Services.Jobs.Jobs(t.Context(), p, scheduler.Query{AssistantID: a.ID})
	need(t, err)
	if len(jobs.Items) != 1 || jobs.Items[0].State != scheduler.Retry {
		t.Fatalf("retry not persisted: %+v", jobs)
	}
	return jobs.Items[0]
}
func TestModelRetrySurvivesRuntimeRestart(t *testing.T) {
	var calls atomic.Int32
	b, p, a, cfg := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			io.WriteString(w, `{}`)
			return
		}
		io.WriteString(w, `{"id":"ok","choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	})
	first := deferFirstRetry(t, b, p, a)
	need(t, b.Close())
	restarted, err := Build(t.Context(), cfg)
	need(t, err)
	defer restarted.Close()
	jobs, err := restarted.Services.Jobs.Jobs(t.Context(), p, scheduler.Query{AssistantID: a.ID})
	need(t, err)
	if jobs.Items[0].Attempts != 1 || !jobs.Items[0].NotBefore.Equal(first.NotBefore) || calls.Load() != 1 {
		t.Fatal("restart reset retry")
	}
	job := awaitRetryJob(t, restarted, p, a)
	if job.State != scheduler.Done || job.ExecutionID != first.ExecutionID || job.Attempts != 2 || calls.Load() != 2 {
		t.Fatalf("restart lost retry: %+v calls=%d", job, calls.Load())
	}
}
func TestModelRetryStopAndRevocationPreventAnotherRequest(t *testing.T) {
	for _, action := range []string{"stop", "revoke"} {
		t.Run(action, func(t *testing.T) {
			var calls atomic.Int32
			b, p, a, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(429)
				io.WriteString(w, `{}`)
			})
			first := deferFirstRetry(t, b, p, a)
			if action == "stop" {
				need(t, b.Services.Agent.Stop(t.Context(), p, a.ID))
				_, err := scheduler.NewQueue(b.store).Claim(t.Context(), first.NotBefore.Add(time.Second))
				if core.CodeOf(err) != core.NotFound {
					t.Fatalf("stop left retry eligible: %v", err)
				}
				status, err := b.Services.Agent.Status(t.Context(), p, a.ID)
				need(t, err)
				if status.ModelRetry != nil {
					t.Fatal("stop left stale retry banner")
				}
			} else {
				grants, err := b.Services.Grants.List(t.Context(), p, policy.Query{AssistantID: a.ID})
				need(t, err)
				for _, g := range grants.Items {
					need(t, b.Services.Grants.Revoke(t.Context(), p, policy.RevokeRequest{Mutation: core.Mutation{IdempotencyKey: "revoke/" + string(g.ID), ExpectedVersion: g.Version}, GrantID: g.ID}))
				}
				job := awaitRetryJob(t, b, p, a)
				if job.State != scheduler.Failed {
					t.Fatalf("revoked retry not failed: %+v", job)
				}
			}
			if calls.Load() != 1 {
				t.Fatalf("request dispatched after %s", action)
			}
		})
	}
}

func TestExpediteSupersedesSleepingRetryWithoutWaitingForDelay(t *testing.T) {
	var calls atomic.Int32
	b, p, a, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(429)
			io.WriteString(w, `{}`)
			return
		}
		io.WriteString(w, `{"id":"ok","choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	})
	first := deferFirstRetry(t, b, p, a)
	message, err := b.Services.Conversations.SendUser(t.Context(), p, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "urgent"}, ConversationID: a.ConversationID, Content: conversation.Content{Kind: "text", Text: "Urgent correction"}})
	need(t, err)
	_, err = b.Services.Agent.Expedite(t.Context(), p, a.ID, agent.ExpediteRequest{Mutation: core.Mutation{IdempotencyKey: "expedite"}, ConversationID: a.ConversationID, ThroughSequence: message.Sequence})
	need(t, err)
	claim, err := scheduler.NewQueue(b.store).Claim(t.Context(), time.Now())
	need(t, err)
	if claim.Job.Event.ID != first.Event.ID || claim.Job.NotBefore.After(time.Now()) {
		t.Fatal("expedite did not wake original turn")
	}
	_, err = b.Services.Agent.(agent.Runtime).Handle(t.Context(), agent.Turn{Principal: claim.Principal, Event: claim.Job.Event, Attempt: claim.Job.Attempts})
	need(t, err)
	if calls.Load() != 3 {
		t.Fatal("unexpected calls", calls.Load())
	}
}

func TestCanceledModelCallReleasesLocalHoldWithoutRetry(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	b, p, a, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { close(started); <-release })
	defer close(release)
	need(t, event.NewWriter(b.store).AppendAssistant(t.Context(), event.AssistantEvent{ID: "cancel-quota", OwnerID: p.OwnerID, AssistantID: a.ID, Kind: event.ResumeRequested, CreatedAt: time.Now()}))
	ctx, cancel := context.WithCancel(t.Context())
	worker := b.Services.Jobs.(scheduler.Service)
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	defer cancel()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("model did not start")
	}
	usage, err := quota.ModelTotals(t.Context(), b.store, p.OwnerID, a.ModelID, time.Time{}, time.Time{})
	need(t, err)
	if usage.Active != 1 || len(usage.Reserved) != 1 {
		t.Fatalf("running model lacks hold: %+v", usage)
	}
	cancel()
	select {
	case err := <-done:
		need(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("canceled worker did not stop")
	}
	usage, err = quota.ModelTotals(t.Context(), b.store, p.OwnerID, a.ModelID, time.Time{}, time.Time{})
	need(t, err)
	if usage.Active != 0 || len(usage.Reserved) != 0 || usage.Unknown != 1 || usage.Calls != 1 || len(usage.Spent) != 0 {
		t.Fatalf("canceled call retained hold or lost unknown evidence: %+v", usage)
	}
}
