package app

import (
	"bytes"
	"fmt"
	"io"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/modelcatalog"
	"molr.ai/runtime/internal/quota"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
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
	a := adapters{store: b.store, pricing: b.Pricing, reserveModelCost: true, now: func() time.Time { return time.Date(2026, 9, 24, 5, 0, 0, 0, time.UTC) }}
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
	need(t, checkReservation(ctx, b.store, access{b.store}, req, provider, true))
	provider.ReservePricing = &model.Pricing{Currency: "USD", InputMicrosPerMillion: 300000, OutputMicrosPerMillion: 1200000}
	if err := checkReservation(ctx, b.store, access{b.store}, req, provider, true); core.CodeOf(err) != core.BudgetExceeded {
		t.Fatalf("stale reservation accepted: %v", err)
	}
}

func TestModelCostReservationSwitchDispatchesAndSettles(t *testing.T) {
	for _, currency := range []string{"USD", "CNY"} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/enabled=%t", currency, enabled), func(t *testing.T) {
				ctx := t.Context()
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					call := calls.Add(1)
					tokens := []int{240000, 760000, 10000}[call-1]
					fmt.Fprintf(w, `{"id":"reply-%d","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":%d,"completion_tokens":0}}`, call, tokens)
				}))
				defer server.Close()
				cfg := testConfig(t)
				cfg.Pricing.ReserveModelCost = enabled
				b, err := Build(ctx, cfg)
				need(t, err)
				defer b.Close()
				p, _ := ownerSession(t, b)
				binding := resource.Binding{Endpoint: server.URL, AllowedOrigins: []string{server.URL}, Models: []resource.ModelConfig{{Name: "priced-model", ContextLimit: 2000000, SupportsTools: true, Pricing: &resource.ModelPricing{Currency: currency, InputMicrosPerMillion: 1000000}}}}
				r, err := b.Services.Resources.Connect(ctx, p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "priced-provider"}, Kind: resource.Model, Name: "Priced provider", Provider: "openai-compatible", Binding: binding})
				need(t, err)
				id := modelcatalog.Entries(r, binding)[0].ID
				a, err := b.Services.Assistants.Hire(ctx, p, assistant.HireRequest{Mutation: core.Mutation{IdempotencyKey: "priced-assistant"}, Name: "Budget test", ModelID: id, Budget: core.AssistantBudget{Limit: core.Money{Currency: currency, Micros: 1000000}, Period: "daily"}})
				need(t, err)
				claim, err := scheduler.NewQueue(b.store).Claim(ctx, time.Now())
				need(t, err)
				adapter := adapters{store: b.store, pricing: b.Pricing, reserveModelCost: cfg.Pricing.ReserveModelCost}
				inputs := []model.Input{{Role: model.User, Text: "hello"}}
				estimate, err := adapter.estimate(ctx, id, inputs, 16)
				need(t, err)
				if estimate.Currency != currency || (!enabled && estimate.Micros != 0) || (enabled && estimate.Micros != 2000000) {
					t.Fatalf("estimate: %+v", estimate)
				}
				budget := b.Services.Budget.(policy.Budget)
				for i := 0; i < 4; i++ {
					e := core.Execution{Principal: claim.Principal, AssistantID: a.ID, OperationID: core.ID(fmt.Sprintf("call-%d", i))}
					hold, err := budget.Reserve(ctx, policy.ReserveRequest{Execution: e, ModelID: id, Estimate: estimate})
					if enabled || i == 3 {
						if core.CodeOf(err) != core.BudgetExceeded {
							t.Fatalf("expected budget rejection, got %v", err)
						}
						break
					}
					need(t, err)
					stream, err := b.Services.Models.(model.Client).Generate(ctx, model.Request{Execution: e, ModelID: id, ReservationID: hold.ID, Inputs: inputs, MaxOutputTokens: 16})
					need(t, err)
					for {
						_, err = stream.Next(ctx)
						if err == io.EOF {
							break
						}
						need(t, err)
					}
					need(t, stream.Close())
					// Replaying settlement must not double-charge the actual provider receipt.
					receipt, err := model.NewReceipts(b.store).ByOperation(ctx, p.OwnerID, e.OperationID)
					need(t, err)
					need(t, settleReceipt(ctx, b.store, budget, e, receipt))
					usage, err := budget.Usage(ctx, p, policy.UsageQuery{AssistantID: a.ID, CurrentPeriod: true})
					need(t, err)
					if len(usage.OriginalSpent) != 1 || usage.OriginalSpent[0] != (core.Money{Currency: currency, Micros: []int64{240000, 1000000, 1010000}[i]}) || usage.Reserved.Micros != 0 {
						t.Fatalf("usage: %+v", usage)
					}
				}
				if (enabled && calls.Load() != 0) || (!enabled && calls.Load() != 3) {
					t.Fatalf("provider calls: %d", calls.Load())
				}
			})
		}
	}
}

func TestZeroEstimateStillEnforcesGroupModelBudget(t *testing.T) {
	f := newMultiUserFixture(t, func(c *config.Config) { c.Pricing.ReserveModelCost = false })
	a := f.assistant(t, f.alice, "group-budget")
	adapter := adapters{store: f.b.store, pricing: f.b.Pricing}
	estimate, err := adapter.estimate(t.Context(), f.model, nil, 16)
	need(t, err)
	if estimate.Micros != 0 {
		t.Fatal(estimate)
	}
	budget := f.b.Services.Budget.(policy.Budget)
	e := f.execution(t, f.alice, a, "first")
	hold, err := budget.Reserve(t.Context(), policy.ReserveRequest{Execution: e, ModelID: f.model, Estimate: estimate})
	need(t, err)
	// Seed a confirmed charge through the accounting repository; this test isolates group admission.
	_, err = (&quota.Budgets{Store: f.b.store}).Settle(t.Context(), f.alice.OwnerID, quota.SettleRequest{ReservationID: hold.ID, OperationID: e.OperationID, ReceiptID: "receipt", Actual: core.Money{Currency: "USD", Micros: 1010000}})
	need(t, err)
	e = f.execution(t, f.alice, a, "next")
	_, err = budget.Reserve(t.Context(), policy.ReserveRequest{Execution: e, ModelID: f.model, Estimate: estimate})
	if core.ReasonOf(err) != "group_quota_exceeded" {
		t.Fatalf("group budget bypassed: %v", err)
	}
}

func TestZeroEstimateStillRequiresModelPricing(t *testing.T) {
	_, err := estimateProvider(model.Provider{}, 16, false)
	if core.ReasonOf(err) != core.ModelPricingMissing {
		t.Fatalf("unknown price treated as free: %v", err)
	}
}
