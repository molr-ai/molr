package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
)

func TestDraftProviderConnectionUsesRealNetworkWithoutSaving(t *testing.T) {
	b, err := Build(t.Context(), testConfig(t))
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer transient-key" {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"one"},{"id":"two"}]}`))
	}))
	defer server.Close()
	req := resource.TestConnectionRequest{Provider: "openai-compatible", Binding: resource.Binding{Endpoint: server.URL + "/v1/chat/completions", AllowedOrigins: []string{server.URL}}, APIKey: core.NewSecret("transient-key")}
	result, err := b.Services.Resources.TestConnection(t.Context(), p, req)
	need(t, err)
	if result.ModelCount != 2 || calls != 1 {
		t.Fatalf("unexpected test result: %#v, %d calls", result, calls)
	}
	resources, err := b.Services.Resources.List(t.Context(), p, resource.Query{})
	need(t, err)
	credentials, err := b.Services.Credentials.List(t.Context(), p, core.PageRequest{})
	need(t, err)
	if len(resources.Items) != 0 || len(credentials.Items) != 0 {
		t.Fatal("connection test persisted resources or credentials")
	}
	_, err = b.Services.Resources.TestConnection(t.Context(), core.Principal{}, req)
	if err == nil || calls != 1 {
		t.Fatal("anonymous probe dispatched")
	}
	req.APIKey = core.NewSecret("bad-key")
	_, err = b.Services.Resources.TestConnection(t.Context(), p, req)
	if err == nil {
		t.Fatal("rejected key reported success")
	}
	req.Binding.Endpoint = "http://127.0.0.2/v1/chat/completions"
	req.Binding.AllowedOrigins = []string{"http://127.0.0.2"}
	_, err = b.Services.Resources.TestConnection(t.Context(), p, req)
	if err == nil || calls != 2 {
		t.Fatal("probe bypassed deployment network policy")
	}
}
