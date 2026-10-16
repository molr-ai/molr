package app

import (
	"fmt"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/buildinfo"
	"molr.ai/runtime/internal/platform"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
)

func TestProviderConnectionDiscoversMultipleModelsWithoutManualConfiguration(t *testing.T) {
	b, err := Build(t.Context(), testConfig(t))
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.UserAgent(), "molr/"+buildinfo.Version; got != want {
			t.Errorf("User-Agent = %q, want %q", got, want)
		}
		calls++
		if r.Method != "GET" || r.URL.Path != "/v1/models" {
			t.Errorf("unexpected discovery request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"chat-one"},{"id":"chat-two"},{"id":"chat-one"}]}`))
	}))
	defer server.Close()
	r, err := b.Services.Resources.Connect(t.Context(), p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "provider"}, Kind: resource.Model, Name: "Provider", Provider: "openai-compatible", Binding: resource.Binding{Endpoint: server.URL + "/v1/chat/completions", AllowedOrigins: []string{server.URL}}})
	need(t, err)
	rows, err := b.Services.Models.List(t.Context(), p)
	need(t, err)
	if len(rows) != 0 {
		t.Fatal("connection without discovery invented a model")
	}
	r, err = b.Services.Resources.RefreshModels(t.Context(), p, resource.RefreshModelsRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "discover", ExpectedVersion: r.Version}})
	need(t, err)
	rows, err = b.Services.Models.List(t.Context(), p)
	need(t, err)
	if calls != 1 || len(rows) != 2 || rows[0].ResourceID != r.ID || rows[0].ID == rows[1].ID {
		t.Fatalf("invalid catalog: %#v", rows)
	}
	if rows[0].Pricing != nil {
		t.Fatal("discovery must not invent free pricing")
	}
	if _, err := (adapters{store: b.store}).estimate(t.Context(), rows[0].ID, nil, 10); core.CodeOf(err) != core.FailedPrecondition {
		t.Fatal("unknown prices allowed an estimate")
	}
	id := rows[0].ID
	r, err = b.Services.Resources.RefreshModels(t.Context(), p, resource.RefreshModelsRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "refresh", ExpectedVersion: r.Version}})
	need(t, err)
	rows, err = b.Services.Models.List(t.Context(), p)
	need(t, err)
	if rows[0].ID != id {
		t.Fatal("model identity changed after refresh")
	}
	need(t, b.Services.Resources.Disconnect(t.Context(), p, resource.DisconnectRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "disconnect", ExpectedVersion: r.Version}}))
	rows, err = b.Services.Models.List(t.Context(), p)
	need(t, err)
	if len(rows) != 0 {
		t.Fatal("disconnected provider models remained available")
	}
}

func TestProviderDiscoveryUsesBoundCredentialAndRejectsPrivateDestinations(t *testing.T) {
	b, err := Build(t.Context(), testConfig(t))
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	proof, err := b.Services.Auth.Reauthenticate(t.Context(), p, auth.ReauthenticateRequest{Purpose: "credential.put", Password: core.NewSecret("correct-horse-battery-staple-2026!")})
	need(t, err)
	key, err := b.Services.Credentials.Put(t.Context(), p, credential.PutRequest{Mutation: core.Mutation{IdempotencyKey: "key"}, Provider: "openai-compatible", Label: "test", Value: core.NewSecret("discovery-test-secret"), Verification: proof})
	need(t, err)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer discovery-test-secret" {
			t.Error("bound credential missing")
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"model"}]}`))
	}))
	defer server.Close()
	r, err := b.Services.Resources.Connect(t.Context(), p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "provider"}, Kind: resource.Model, Name: "provider", Provider: "openai-compatible", Binding: resource.Binding{Endpoint: server.URL + "/v1/chat/completions", AllowedOrigins: []string{server.URL}, CredentialID: key.ID}})
	need(t, err)
	_, err = b.Services.Resources.RefreshModels(t.Context(), p, resource.RefreshModelsRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "discover", ExpectedVersion: r.Version}})
	need(t, err)
	if calls != 1 {
		t.Fatal("discovery did not reach provider")
	}
	privateCalls := 0
	private := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { privateCalls++; w.WriteHeader(200) }))
	defer private.Close()
	cfg := testConfig(t)
	cfg.Network.AllowedPrivateIPs = nil
	blocked, err := Build(t.Context(), cfg)
	need(t, err)
	defer blocked.Close()
	blockedOwner, _ := ownerSession(t, blocked)
	r, err = blocked.Services.Resources.Connect(t.Context(), blockedOwner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "private"}, Kind: resource.Model, Name: "private", Provider: "openai-compatible", Binding: resource.Binding{Endpoint: private.URL + "/v1/chat/completions", AllowedOrigins: []string{private.URL}}})
	need(t, err)
	if _, err = blocked.Services.Resources.RefreshModels(t.Context(), blockedOwner, resource.RefreshModelsRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "blocked", ExpectedVersion: r.Version}}); err == nil || privateCalls != 0 {
		t.Fatal("unapproved private destination reached")
	}
}

func TestProviderDiscoveryRejectsUnauthenticatedCallsAndRedirects(t *testing.T) {
	b, err := Build(t.Context(), testConfig(t))
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	leaked := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked++; w.WriteHeader(200) }))
	defer target.Close()
	calls := 0
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()
	r, err := b.Services.Resources.Connect(t.Context(), p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "provider"}, Kind: resource.Model, Name: "Provider", Provider: "openai-compatible", Binding: resource.Binding{Endpoint: source.URL + "/v1/chat/completions", AllowedOrigins: []string{source.URL}}})
	need(t, err)
	request := resource.RefreshModelsRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "refresh", ExpectedVersion: r.Version}}
	if _, err = b.Services.Resources.RefreshModels(t.Context(), core.Principal{}, request); err == nil || calls != 0 {
		t.Fatal("unauthenticated discovery dispatched")
	}
	if _, err = b.Services.Resources.RefreshModels(t.Context(), p, request); err == nil || leaked != 0 {
		t.Fatal("discovery followed a redirect")
	}
	current, err := b.Services.Resources.Get(t.Context(), p, r.ID)
	need(t, err)
	if current.Version != r.Version {
		t.Fatal("failed discovery changed the catalog version")
	}
}

func TestDiscoveryReadsDeclaredModalitiesWithoutGuessing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"vision","input_modalities":["text","image"],"context_window":65536},{"id":"audio","input_modalities":["text","audio"]},{"id":"unknown-vision-model"}]}`))
	}))
	defer server.Close()
	rows, err := fetchProviderModels(t.Context(), server.Client(), server.URL, core.Secret{}, "")
	need(t, err)
	if len(rows) != 3 || !rows[0].SupportsImages || rows[0].ProviderContextLimit != 65536 || rows[0].ContextLimit != 0 || !rows[1].SupportsAudio || rows[2].SupportsImages || rows[2].SupportsAudio {
		t.Fatalf("incorrect capabilities: %+v", rows)
	}
}

func TestOutputLimitsDiscoveryOverridesRefreshAndClear(t *testing.T) {
	declared := 8192
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"data":[{"id":"declared","max_output_tokens":%d},{"id":"completion","max_completion_tokens":4096},{"id":"nested","top_provider":{"max_completion_tokens":16384}},{"id":"unknown"},{"id":"invalid","max_output_tokens":-1}]}`, declared)
	}))
	defer server.Close()
	b, err := Build(t.Context(), testConfig(t))
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	r, err := b.Services.Resources.Connect(t.Context(), p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "output-provider"}, Kind: resource.Model, Name: "output", Provider: "openai-compatible", Binding: resource.Binding{Endpoint: server.URL + "/v1/chat/completions", AllowedOrigins: []string{server.URL}}})
	need(t, err)
	refresh := func(key string) {
		t.Helper()
		r, err = b.Services.Resources.RefreshModels(t.Context(), p, resource.RefreshModelsRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: key, ExpectedVersion: r.Version}})
		need(t, err)
	}
	lookup := func(name string) model.Model {
		t.Helper()
		rows, err := b.Services.Models.List(t.Context(), p)
		need(t, err)
		for _, m := range rows {
			if m.Name == name {
				return m
			}
		}
		t.Fatalf("missing %s", name)
		return model.Model{}
	}
	refresh("discover")
	for name, want := range map[string]int{"declared": 8192, "completion": 4096, "nested": 16384, "unknown": 32768, "invalid": 32768} {
		if m := lookup(name); m.EffectiveOutputLimit != want || m.OutputLimit != 0 {
			t.Fatalf("%s: %+v", name, m)
		}
	}
	configure := func(key string, limit int) {
		t.Helper()
		r, err = b.Services.Resources.ConfigureModel(t.Context(), p, resource.ConfigureModelRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: key, ExpectedVersion: r.Version}, Model: resource.ModelConfig{Name: "declared", SupportsTools: true, OutputLimit: limit, ProviderOutputLimit: 777}})
		need(t, err)
	}
	configure("override", 2048)
	if m := lookup("declared"); m.EffectiveOutputLimit != 2048 || m.OutputLimitSource != "manual" || m.ProviderOutputLimit != 8192 {
		t.Fatalf("manual resolution: %+v", m)
	}
	declared = 65536
	refresh("new-catalog")
	if m := lookup("declared"); m.OutputLimit != 2048 || m.ProviderOutputLimit != 65536 || m.EffectiveOutputLimit != 2048 {
		t.Fatalf("refresh lost override: %+v", m)
	}
	configure("clear", 0)
	if m := lookup("declared"); m.OutputLimit != 0 || m.EffectiveOutputLimit != 65536 || m.OutputLimitSource != "provider" {
		t.Fatalf("clear did not restore provider: %+v", m)
	}
	for _, bad := range []int{-1, 1000001} {
		_, err = b.Services.Resources.ConfigureModel(t.Context(), p, resource.ConfigureModelRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: fmt.Sprint(bad), ExpectedVersion: r.Version}, Model: resource.ModelConfig{Name: "declared", OutputLimit: bad}})
		if core.CodeOf(err) != core.InvalidArgument {
			t.Fatalf("accepted %d: %v", bad, err)
		}
	}
}

func TestModelTransportUsesTenMinuteTimeout(t *testing.T) {
	client, err := (adapters{}).client(t.Context(), model.Request{}, model.Provider{Endpoint: "https://example.com/v1/chat/completions"})
	need(t, err)
	defer client.CloseIdleConnections()
	if client.Timeout != 10*time.Minute || client.Transport.(*http.Transport).ResponseHeaderTimeout != 10*time.Minute {
		t.Fatal("model request can still be interrupted by a shorter transport timeout")
	}
}

func TestCompatibleProviderPresetsSaveDiscoverAndRefresh(t *testing.T) {
	for _, preset := range []string{"vllm", "sglang", "bailian", "zhipu", "moonshot"} {
		for _, useKey := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/key=%t", preset, useKey), func(t *testing.T) {
				b, err := Build(t.Context(), testConfig(t))
				need(t, err)
				defer b.Close()
				p, _ := ownerSession(t, b)
				limit := 65536
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					wantAuth := ""
					if useKey {
						wantAuth = "Bearer self-hosted-key"
					}
					if r.Method != "GET" || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != wantAuth {
						t.Errorf("unexpected discovery request: %s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
					}
					fmt.Fprintf(w, `{"data":[{"id":"served-model","max_model_len":%d}]}`, limit)
				}))
				defer server.Close()
				req := resource.SaveProviderRequest{Name: preset, PricingProvider: preset, Endpoint: server.URL + "/v1/chat/completions", UseAPIKey: useKey, Mutation: core.Mutation{IdempotencyKey: "save"}}
				if useKey {
					req.APIKey = core.NewSecret("self-hosted-key")
					req.Verification, err = b.Services.Auth.Reauthenticate(t.Context(), p, auth.ReauthenticateRequest{Purpose: "credential.put", Password: core.NewSecret("correct-horse-battery-staple-2026!")})
					need(t, err)
				}
				r, err := b.Services.Resources.SaveProvider(t.Context(), p, req)
				need(t, err)
				connection, err := b.Services.Resources.ProviderConnection(t.Context(), p, r.ID)
				need(t, err)
				if connection.PricingProvider != preset || (connection.CredentialID != "") != useKey {
					t.Fatalf("connection: %+v", connection)
				}
				r, err = b.Services.Resources.RefreshModels(t.Context(), p, resource.RefreshModelsRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "discover", ExpectedVersion: r.Version}})
				need(t, err)
				rows, err := b.Services.Models.List(t.Context(), p)
				need(t, err)
				if len(rows) != 1 || rows[0].EffectiveContextLimit != limit || rows[0].ContextLimitSource != "provider" || rows[0].Pricing != nil {
					t.Fatalf("models: %+v", rows)
				}
				r, err = b.Services.Resources.ConfigureModel(t.Context(), p, resource.ConfigureModelRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "override", ExpectedVersion: r.Version}, Model: resource.ModelConfig{Name: "served-model", SupportsTools: true, ContextLimit: 32768}})
				need(t, err)
				limit = 131072
				_, err = b.Services.Resources.RefreshModels(t.Context(), p, resource.RefreshModelsRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "refresh", ExpectedVersion: r.Version}})
				need(t, err)
				rows, err = b.Services.Models.List(t.Context(), p)
				need(t, err)
				if len(rows) != 1 || rows[0].EffectiveContextLimit != 32768 || rows[0].ProviderContextLimit != limit || rows[0].ContextLimitSource != "manual" {
					t.Fatalf("refresh lost override: %+v", rows)
				}
			})
		}
	}
}

func TestDiscoveryContextWindowPrecedenceAndBounds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"priority","context_window":8192,"max_model_len":65536},{"id":"fallback","context_window":-1,"max_model_len":4096},{"id":"negative","max_model_len":-1},{"id":"too-small","max_model_len":255},{"id":"too-large","max_model_len":2000001},{"id":"missing"},{"id":"null","max_model_len":null}]}`))
	}))
	defer server.Close()
	rows, err := fetchProviderModels(t.Context(), server.Client(), server.URL, core.Secret{}, "")
	need(t, err)
	want := []int{8192, 4096, 0, 0, 0, 0, 0}
	if len(rows) != len(want) {
		t.Fatalf("models: %+v", rows)
	}
	for i, row := range rows {
		if row.ProviderContextLimit != want[i] {
			t.Errorf("%s: %d, want %d", row.Name, row.ProviderContextLimit, want[i])
		}
	}
}

func TestProviderNetworkRejectionExplainsTestAndSave(t *testing.T) {
	for _, allowed := range [][]string{{"127.0.0.1"}, {}} {
		cfg := testConfig(t)
		cfg.Network.AllowedPrivateIPs = allowed
		b, err := Build(t.Context(), cfg)
		need(t, err)
		defer b.Close()
		p, _ := ownerSession(t, b)
		for _, tc := range []struct {
			endpoint string
			reason   core.Reason
		}{
			{"http://192.168.50.20:8000/v1/chat/completions", core.NetworkPrivateAddressDenied},
			{"http://[::ffff:192.168.50.20]:8000/v1/chat/completions", core.NetworkPrivateAddressDenied},
			{"http://example.com/v1/chat/completions", core.NetworkHTTPNotAllowed},
		} {
			// A hostname with a non-empty allowlist is checked after DNS resolution.
			if len(allowed) != 0 && tc.reason == core.NetworkHTTPNotAllowed {
				continue
			}
			u, _ := url.Parse(tc.endpoint)
			_, err = b.Services.Resources.TestConnection(t.Context(), p, resource.TestConnectionRequest{Provider: "openai-compatible", Binding: resource.Binding{Endpoint: tc.endpoint, AllowedOrigins: []string{u.Scheme + "://" + u.Host}}})
			if core.CodeOf(err) != core.InvalidArgument || core.ReasonOf(err) != tc.reason {
				t.Fatalf("test: %v, reason=%s", err, core.ReasonOf(err))
			}
			_, err = b.Services.Resources.SaveProvider(t.Context(), p, resource.SaveProviderRequest{Name: "Local provider", PricingProvider: "vllm", Endpoint: tc.endpoint, Mutation: core.Mutation{IdempotencyKey: string(tc.reason)}})
			if core.CodeOf(err) != core.InvalidArgument || core.ReasonOf(err) != tc.reason {
				t.Fatalf("save: %v, reason=%s", err, core.ReasonOf(err))
			}
		}
	}
}

func TestDiscoveryPreservesNetworkReasonAfterDial(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("blocked endpoint was contacted") }))
	defer server.Close()
	client := platform.ConfiguredHTTPClient(server.URL, nil)
	defer client.CloseIdleConnections()
	_, err := fetchProviderModels(t.Context(), client, server.URL+"/v1/chat/completions", core.Secret{}, "")
	if core.ReasonOf(err) != core.NetworkPrivateAddressDenied {
		t.Fatalf("lost network reason: %v", err)
	}
}
