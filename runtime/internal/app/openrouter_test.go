package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
)

func TestOpenRouterCatalogRefreshPreservesManualOverrides(t *testing.T) {
	price := "0.000002"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" {
			t.Errorf("wrong path %s", r.URL.Path)
		}
		fmt.Fprintf(w, `{"data":[{"id":"vendor/chat","context_length":128000,"architecture":{"input_modalities":["text","image"],"output_modalities":["text"]},"supported_parameters":["tools"],"top_provider":{"max_completion_tokens":8192},"pricing":{"prompt":%q,"completion":"0.000008","input_cache_read":"0.000001"}},{"id":"vendor/image","architecture":{"output_modalities":["image"]}}]}`, price)
	}))
	defer server.Close()
	b, err := Build(t.Context(), testConfig(t))
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	r, err := b.Services.Resources.SaveProvider(t.Context(), p, resource.SaveProviderRequest{Name: "OpenRouter", PricingProvider: "openrouter", Endpoint: server.URL + "/api/v1/chat/completions", Mutation: core.Mutation{IdempotencyKey: "connect"}})
	need(t, err)
	refresh := func(key string) {
		r, err = b.Services.Resources.RefreshModels(t.Context(), p, resource.RefreshModelsRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: key, ExpectedVersion: r.Version}})
		need(t, err)
	}
	refresh("discover")
	models, err := b.Services.Models.List(t.Context(), p)
	need(t, err)
	if len(models) != 1 {
		t.Fatalf("wrong catalog: %+v", models)
	}
	m := models[0]
	if m.ProviderContextLimit != 128000 || m.ProviderOutputLimit != 8192 || !m.SupportsTools || !m.SupportsImages || m.PricingInfo.Source != "provider" || m.Pricing.InputMicrosPerMillion != 2_000_000 {
		t.Fatalf("lost metadata: %+v", m)
	}
	r, err = b.Services.Resources.ConfigureModel(t.Context(), p, resource.ConfigureModelRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "override", ExpectedVersion: r.Version}, Model: resource.ModelConfig{Name: "vendor/chat", ContextLimit: 65536, Pricing: &resource.ModelPricing{Currency: "USD", InputMicrosPerMillion: 10}, ProviderPricing: &resource.ModelPricing{Currency: "USD"}}})
	need(t, err)
	price = "0.000003"
	refresh("refresh")
	models, err = b.Services.Models.List(t.Context(), p)
	need(t, err)
	m = models[0]
	if m.EffectiveContextLimit != 65536 || m.SupportsTools || m.PricingInfo.Source != "manual" || m.Pricing.InputMicrosPerMillion != 10 || m.PricingInfo.Automatic.InputMicrosPerMillion != 3_000_000 {
		t.Fatalf("refresh lost override: %+v", m)
	}
	r, err = b.Services.Resources.ConfigureModel(t.Context(), p, resource.ConfigureModelRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "clear", ExpectedVersion: r.Version}, Model: resource.ModelConfig{Name: "vendor/chat"}})
	need(t, err)
	models, err = b.Services.Models.List(t.Context(), p)
	need(t, err)
	if models[0].PricingInfo.Source != "provider" || models[0].Pricing.InputMicrosPerMillion != 3_000_000 {
		t.Fatal("clearing manual price did not restore catalog")
	}
	r, err = b.Services.Resources.SaveProvider(t.Context(), p, resource.SaveProviderRequest{ID: r.ID, Name: "Other endpoint", PricingProvider: "openrouter", Endpoint: server.URL + "/other/chat/completions", Mutation: core.Mutation{IdempotencyKey: "move", ExpectedVersion: r.Version}})
	need(t, err)
	models, err = b.Services.Models.List(t.Context(), p)
	need(t, err)
	if len(models) != 1 || models[0].Pricing != nil {
		t.Fatal("catalog price leaked across endpoints")
	}

}

func TestOpenRouterCatalogBoundsAndCapabilities(t *testing.T) {
	count := 600
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rows := make([]any, count)
		for i := range rows {
			rows[i] = map[string]any{"id": fmt.Sprint("vendor/", i), "architecture": map[string]any{"output_modalities": []string{"text"}}, "pricing": map[string]string{"prompt": "0", "completion": "0"}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": rows})
	}))
	defer server.Close()
	rows, err := fetchProviderModels(t.Context(), server.Client(), server.URL, core.Secret{}, "openrouter")
	need(t, err)
	if len(rows) != 600 || rows[0].SupportsTools || rows[0].ProviderPricing == nil || rows[0].ProviderPricing.InputMicrosPerMillion != 0 {
		t.Fatal("wrong large catalog")
	}
	if _, err := fetchProviderModels(t.Context(), server.Client(), server.URL, core.Secret{}, "custom"); err == nil {
		t.Fatal("generic catalog bound changed")
	}
	count = resource.MaxProviderModels + 1
	if _, err := fetchProviderModels(t.Context(), server.Client(), server.URL, core.Secret{}, "openrouter"); err == nil {
		t.Fatal("unbounded router catalog")
	}
}

func TestOpenRouterPricingDoesNotInventFreeUsage(t *testing.T) {
	for _, prices := range []map[string]string{
		nil, {"prompt": "0"}, {"prompt": "-1", "completion": "0"}, {"prompt": "NaN", "completion": "0"},
		{"prompt": "9999999999999999999999", "completion": "0"}, {"prompt": "0.000001", "completion": "0.000002", "request": "0.01"},
		{"prompt": "0.000001", "completion": "0.000002", "input_cache_read": "0.000003"},
	} {
		if openRouterPricing(prices) != nil {
			t.Fatalf("accepted incomplete/invalid price %v", prices)
		}
	}
	p := openRouterPricing(map[string]string{"prompt": "0.0000000000001", "completion": "0.000002"})
	if p == nil || p.InputMicrosPerMillion != 1 || p.CachedInputMicrosPerMillion != 1 {
		t.Fatal("fractional price rounded down or absent cache price became free")
	}
}

func TestOpenRouterStructuredPricingDoesNotRejectCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"vendor/conditional","architecture":{"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000002","overrides":[{"condition":"large-context","prompt":"0.000003"}]}},{"id":"vendor/plain","architecture":{"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000002"}}]}`)
	}))
	defer server.Close()
	rows, err := fetchProviderModels(t.Context(), server.Client(), server.URL, core.Secret{}, "openrouter")
	need(t, err)
	if len(rows) != 2 || rows[0].ProviderPricing != nil || rows[1].ProviderPricing == nil {
		t.Fatalf("structured tariff invalidated catalog: %+v", rows)
	}
}
