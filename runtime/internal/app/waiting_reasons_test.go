package app

import (
	"encoding/json"
	"molr.ai/runtime/internal/modelcatalog"
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
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/tests/testkit"
)

func TestWaitingReasonsDescribeActualBlockers(t *testing.T) {
	for _, reason := range []string{"model_pricing_missing", "model_not_authorized", "budget_exceeded", "model_unavailable", "model_not_configured", "model_authentication_failed", "model_rate_limited"} {
		t.Run(reason, func(t *testing.T) {
			cfg := testConfig(t)
			cfg.Pricing.ReserveModelCost = true
			b, err := Build(t.Context(), cfg)
			need(t, err)
			defer b.Close()
			p, _ := ownerSession(t, b)
			ctx := t.Context()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !testkit.DrainRequestBody(t, r) {
					return
				}
				calls.Add(1)
				code := http.StatusUnauthorized
				if reason == "model_rate_limited" {
					code = http.StatusTooManyRequests
				}
				http.Error(w, "PRIVATE_PROVIDER_RESPONSE", code)
			}))
			defer server.Close()
			pricing := &resource.ModelPricing{Currency: "USD"}
			if reason == "model_pricing_missing" {
				pricing = nil
			}
			if reason == "budget_exceeded" {
				pricing.InputMicrosPerMillion = 1000000
			}
			binding := resource.Binding{Endpoint: server.URL + "/v1/chat/completions", AllowedOrigins: []string{server.URL}, Models: []resource.ModelConfig{{Name: "test", ContextLimit: 32768, SupportsTools: true, Pricing: pricing}}}
			r, err := b.Services.Resources.Connect(ctx, p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "provider"}, Kind: resource.Model, Name: "Provider", Provider: "openai-compatible", Binding: binding})
			need(t, err)
			a, err := b.Services.Assistants.Hire(ctx, p, assistant.HireRequest{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}, Mutation: core.Mutation{IdempotencyKey: "hire"}, Name: "Assistant", ModelID: modelcatalog.Entries(r, binding)[0].ID})
			need(t, err)
			if reason == "model_not_authorized" {
				grants, err := b.Services.Grants.List(ctx, p, policy.Query{AssistantID: a.ID})
				need(t, err)
				g := grants.Items[0]
				need(t, b.Services.Grants.Revoke(ctx, p, policy.RevokeRequest{Mutation: core.Mutation{IdempotencyKey: "revoke", ExpectedVersion: g.Version}, GrantID: g.ID}))
			}
			if reason == "model_unavailable" {
				need(t, b.Services.Resources.Disconnect(ctx, p, resource.DisconnectRequest{Mutation: core.Mutation{IdempotencyKey: "disconnect", ExpectedVersion: r.Version}, ID: r.ID}))
			}
			if reason == "model_not_configured" {
				a.ModelID = ""
				a.Version++
				need(t, assistant.NewRepository(b.store).Save(ctx, a, a.Version-1))
			}
			need(t, event.NewWriter(b.store).AppendAssistant(ctx, event.AssistantEvent{ID: "wake", OwnerID: p.OwnerID, AssistantID: a.ID, Kind: event.ResumeRequested, CreatedAt: time.Now()}))
			claim, err := scheduler.NewQueue(b.store).Claim(ctx, time.Now())
			need(t, err)
			_, err = b.Services.Agent.(agent.Runtime).Handle(ctx, agent.Turn{Principal: claim.Principal, Event: claim.Job.Event})
			if err == nil {
				t.Fatal("expected blocked execution")
			}
			status, err := b.Services.Agent.Status(ctx, p, a.ID)
			need(t, err)
			expectedState := agent.Waiting
			if reason == "model_rate_limited" {
				expectedState = agent.Working
				if status.ModelRetry == nil {
					t.Fatal("missing retry status")
				}
			}
			if status.State != expectedState || status.WaitingReason != reason {
				t.Fatalf("unexpected waiting status: %+v", status)
			}
			encoded, err := json.Marshal(status)
			need(t, err)
			if strings.Contains(string(encoded), "PRIVATE_PROVIDER_RESPONSE") {
				t.Fatal("provider error leaked into status")
			}
			if reason != "model_authentication_failed" && reason != "model_rate_limited" && calls.Load() != 0 {
				t.Fatal("preflight blocker reached provider")
			}
		})
	}
}
