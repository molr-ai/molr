package app

import (
	"fmt"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnthropicSaveDiscoveryPaginationAndProtocol(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/v1/models" || r.Header.Get("x-api-key") != "test-anthropic-key" || r.Header.Get("anthropic-version") != "2023-06-01" || r.Header.Get("Authorization") != "" {
			t.Errorf("incorrect discovery contract: %s", r.URL)
			w.WriteHeader(400)
			return
		}
		if r.URL.Query().Get("after_id") == "first" {
			fmt.Fprint(w, `{"data":[{"id":"second","max_input_tokens":200000,"max_tokens":4096}],"has_more":false}`)
		} else {
			fmt.Fprint(w, `{"data":[{"id":"first","max_input_tokens":100000,"max_tokens":8192}],"has_more":true,"last_id":"first"}`)
		}
	}))
	defer server.Close()
	b, err := Build(t.Context(), testConfig(t))
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	proof, err := b.Services.Auth.Reauthenticate(t.Context(), p, auth.ReauthenticateRequest{Purpose: "credential.put", Password: core.NewSecret("correct-horse-battery-staple-2026!")})
	need(t, err)
	r, err := b.Services.Resources.SaveProvider(t.Context(), p, resource.SaveProviderRequest{Name: "Anthropic", PricingProvider: "anthropic", Endpoint: server.URL + "/v1/messages", UseAPIKey: true, APIKey: core.NewSecret("test-anthropic-key"), Verification: proof, Mutation: core.Mutation{IdempotencyKey: "save"}})
	need(t, err)
	_, err = b.Services.Resources.RefreshModels(t.Context(), p, resource.RefreshModelsRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "refresh", ExpectedVersion: r.Version}})
	need(t, err)
	rows, err := b.Services.Models.List(t.Context(), p)
	need(t, err)
	if requests != 2 || len(rows) != 2 || rows[0].ProviderContextLimit != 100000 || rows[1].ProviderOutputLimit != 4096 || !rows[0].SupportsImages || rows[0].SupportsAudio || rows[0].Pricing != nil {
		t.Fatalf("bad discovery: %+v, requests %d", rows, requests)
	}
	connection, err := b.Services.Resources.ProviderConnection(t.Context(), p, r.ID)
	need(t, err)
	if connection.PricingProvider != "anthropic" || connection.Endpoint != server.URL+"/v1/messages" {
		t.Fatalf("lost connection: %+v", connection)
	}
}
func TestAnthropicDiscoveryRejectsBrokenPagination(t *testing.T) {
	for _, body := range []string{`{"data":[],"has_more":true,"last_id":"x"}`, `{"data":[{"id":"x"}],"has_more":true}`, `{"data":[{"id":"x"}],"has_more":true,"last_id":"x"}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		_, err := fetchProviderModels(t.Context(), server.Client(), server.URL+"/v1/messages", core.Secret{}, "anthropic")
		server.Close()
		if err == nil {
			t.Fatal("accepted broken pagination", body)
		}
	}
}
func TestLMStudioDiscoveryUsesMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v0/models" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"object":"list","data":[{"id":"local-chat","type":"llm","state":"loaded","compatibility_type":"gguf"},{"id":"embedding","type":"embeddings","state":"not-loaded","compatibility_type":"gguf"}]}`)
	}))
	defer server.Close()
	found := probeLocalProvider(t.Context(), server.Client(), server.URL)
	if found == nil || found.Preset != "lmstudio" || len(found.Models) != 1 || found.Models[0] != "local-chat" || found.Endpoint != server.URL+"/v1" {
		t.Fatalf("incorrect LM Studio discovery: %+v", found)
	}
}
