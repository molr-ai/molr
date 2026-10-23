package app

import (
	"bytes"
	"molr.ai/runtime/internal/modelcatalog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/policy"
)

func TestBuiltInPricingAppliesToSavedModelsAndReloads(t *testing.T) {
	b, err := Build(t.Context(), testConfig(t))
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	binding := resource.Binding{Endpoint: "https://api.deepseek.com/v1/chat/completions", AllowedOrigins: []string{"https://api.deepseek.com"}, Models: []resource.ModelConfig{{Name: "deepseek-flash", ContextLimit: 32768, SupportsTools: true}}}
	r, err := b.Services.Resources.Connect(t.Context(), p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "provider"}, Kind: resource.Model, Name: "DeepSeek", Provider: "openai-compatible", Binding: binding})
	need(t, err)
	a := adapters{store: b.store, pricing: b.Pricing, now: func() time.Time { return time.Date(2026, 9, 24, 5, 0, 0, 0, time.UTC) }}
	entry := modelcatalog.Entries(r, binding)[0]
	model, err := a.provider(t.Context(), p, entry.ID)
	need(t, err)
	if model.Pricing == nil || model.Pricing.InputMicrosPerMillion != 150000 || model.Model.PricingInfo.Source != "builtin" {
		t.Fatalf("missing automatic pricing: %+v", model)
	}
	estimate, err := a.estimate(t.Context(), entry.ID, nil, 1000)
	need(t, err)
	if estimate.Micros != 11031 {
		t.Fatalf("reservation should use peak rates: %+v", estimate)
	}
	data, err := os.ReadFile("../pricing/providers.json")
	need(t, err)
	data = bytes.Replace(data, []byte(`"input_micros_per_million": 150000`), []byte(`"input_micros_per_million": 200000`), 1)
	need(t, b.Pricing.Reload(bytes.NewReader(data)))
	next, err := a.provider(t.Context(), p, entry.ID)
	need(t, err)
	if next.Pricing.InputMicrosPerMillion != 200000 || model.Pricing.InputMicrosPerMillion != 150000 {
		t.Fatal("reload changed old snapshot or failed to update persisted model")
	}
	binding.Models[0].Pricing = &resource.ModelPricing{Currency: "USD", InputMicrosPerMillion: 10, OutputMicrosPerMillion: 20, CachedInputMicrosPerMillion: 1}
	manual := a.modelCatalog().Describe(r, binding, modelcatalog.Entries(r, binding)[0])
	if manual.Pricing.InputMicrosPerMillion != 10 || manual.Model.PricingInfo.Source != "manual" || manual.Model.PricingInfo.Automatic.InputMicrosPerMillion != 200000 {
		t.Fatal("manual override lost")
	}
	binding.Models[0].Pricing = nil
	binding.PricingProvider = "custom"
	if a.modelCatalog().Describe(r, binding, modelcatalog.Entries(r, binding)[0]).Pricing != nil {
		t.Fatal("custom provider received official price")
	}
}

func TestPricingRulesFileStartupFallback(t *testing.T) {
	cfg := testConfig(t)
	cfg.Pricing.RulesFile = filepath.Join(t.TempDir(), "rules.json")
	need(t, os.WriteFile(cfg.Pricing.RulesFile, []byte(`{}`), 0600))
	first, err := Build(t.Context(), cfg)
	need(t, err)
	if _, ok := first.Pricing.Lookup("deepseek", "https://api.deepseek.com", "deepseek-flash", time.Now()); !ok {
		t.Fatal("invalid source lost built-in fallback")
	}
	need(t, first.Close())
	data, err := os.ReadFile("../pricing/providers.json")
	need(t, err)
	need(t, os.WriteFile(cfg.Pricing.RulesFile, data, 0600))
	b, err := Build(t.Context(), cfg)
	need(t, err)
	defer b.Close()
	if _, ok := b.Pricing.Lookup("deepseek", "https://api.deepseek.com", "deepseek-flash", time.Now()); !ok {
		t.Fatal("external rules not loaded")
	}
	cfg.Pricing.RulesFile = "relative.json"
	if cfg.Validate() == nil {
		t.Fatal("relative rules path accepted")
	}
}

func TestReservationRejectsPriceIncreaseBeforeDispatch(t *testing.T) {
	b, err := Build(t.Context(), testConfig(t))
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	ctx := t.Context()
	assistant, err := b.Services.Assistants.Hire(ctx, p, assistant.HireRequest{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}, Mutation: core.Mutation{IdempotencyKey: "hire"}, Name: "Assistant", ModelID: configureTestModel(t, b, p)})
	need(t, err)
	need(t, event.NewWriter(b.store).AppendAssistant(ctx, event.AssistantEvent{ID: "wake", OwnerID: p.OwnerID, AssistantID: assistant.ID, Kind: event.ResumeRequested, CreatedAt: time.Now()}))
	claim, err := scheduler.NewQueue(b.store).Claim(ctx, time.Now())
	need(t, err)
	execution := core.Execution{Principal: claim.Principal, AssistantID: assistant.ID, OperationID: "price-change"}
	reservation, err := b.Services.Budget.(policy.Budget).Reserve(ctx, policy.ReserveRequest{Execution: execution, Estimate: core.Money{Currency: "USD"}})
	need(t, err)
	req := model.Request{Execution: execution, ReservationID: reservation.ID, MaxOutputTokens: 1000}
	provider := model.Provider{Model: model.Model{ContextLimit: 32768}, Pricing: &model.Pricing{Currency: "USD"}}
	need(t, checkReservation(ctx, b.store, access{b.store}, req, provider))
	provider.ReservePricing = &model.Pricing{Currency: "USD", InputMicrosPerMillion: 300000, OutputMicrosPerMillion: 1200000}
	if err := checkReservation(ctx, b.store, access{b.store}, req, provider); core.CodeOf(err) != core.BudgetExceeded {
		t.Fatalf("stale reservation accepted: %v", err)
	}
}
