package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/tests/testkit"
)

func TestNativeModelCurrencySurvivesDispatchSettlementAndPreferences(t *testing.T) {
	b, err := Build(t.Context(), testConfig(t))
	need(t, err)
	defer b.Close()
	ctx := t.Context()
	p, _ := ownerSession(t, b)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !testkit.DrainRequestBody(t, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"native","choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":2}}`))
	}))
	defer server.Close()
	binding := resource.Binding{Endpoint: server.URL, AllowedOrigins: []string{server.URL}, Models: []resource.ModelConfig{{Name: "native", ContextLimit: 32768, SupportsTools: true, Pricing: &resource.ModelPricing{Currency: "CNY", InputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 1000000}}}}
	r, err := b.Services.Resources.Connect(ctx, p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "provider"}, Name: "native", Kind: resource.Model, Provider: "openai-compatible", Binding: binding})
	need(t, err)
	models, err := b.Services.Models.List(ctx, p)
	need(t, err)
	if len(models) != 1 || models[0].Pricing.Currency != "CNY" {
		t.Fatalf("native price lost: %+v", models)
	}
	a, err := b.Services.Assistants.Hire(ctx, p, assistant.HireRequest{Mutation: core.Mutation{IdempotencyKey: "hire"}, Name: "native", ModelID: modelEntries(r, binding)[0].id, Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD", Micros: 1000000}, Period: "daily"}})
	need(t, err)
	need(t, event.NewWriter(b.store).AppendAssistant(ctx, event.AssistantEvent{ID: "wake", OwnerID: p.OwnerID, AssistantID: a.ID, Kind: event.ResumeRequested, CreatedAt: time.Now()}))
	claim, err := scheduler.NewQueue(b.store).Claim(ctx, time.Now())
	need(t, err)
	_, err = b.Services.Agent.(agent.Runtime).Handle(ctx, agent.Turn{Principal: claim.Principal, Event: claim.Job.Event})
	need(t, err)
	receipts, err := persistence.Scan[model.Receipt](ctx, b.store, "agent.model_receipts")
	need(t, err)
	if len(receipts) != 1 || receipts[0].Charged == nil || *receipts[0].Charged != (core.Money{Currency: "CNY", Micros: 10}) || receipts[0].Pricing.Currency != "CNY" {
		t.Fatalf("native receipt lost: %+v", receipts)
	}
	pref, err := b.Services.Settings.Get(ctx, p)
	need(t, err)
	pref.Currency = "CNY"
	pref.Timezone = "Asia/Shanghai"
	_, err = b.Services.Settings.Update(ctx, p, settings.UpdateRequest{Mutation: core.Mutation{IdempotencyKey: "currency", ExpectedVersion: pref.Version}, Settings: pref})
	need(t, err)
	usage, err := b.Services.Budget.Usage(ctx, p, policy.UsageQuery{AssistantID: a.ID, CurrentPeriod: true})
	need(t, err)
	if usage.Spent != (core.Money{Currency: "CNY", Micros: 10}) || len(usage.OriginalSpent) != 1 || usage.Limit.Currency != "USD" {
		t.Fatalf("preference rewrote denomination: %+v", usage)
	}
	persisted, err := b.Services.Assistants.Get(ctx, p, a.ID)
	need(t, err)
	if persisted.Budget != a.Budget {
		t.Fatal("display preference changed budget")
	}
}
