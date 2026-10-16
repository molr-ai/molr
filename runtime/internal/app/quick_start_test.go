package app

import (
	"encoding/json"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/buildinfo"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLANHTTPBrowserAndTransportBoundaries(t *testing.T) {
	cfg := config.Defaults(t.TempDir())
	cfg.HTTP.TLSMode = config.LANHTTP
	cfg.HTTP.Listen = "0.0.0.0:8787"
	cfg.HTTP.PublicURL = "http://192.168.1.20:8787"
	cfg.HTTP.AllowedOrigins = []string{"http://127.0.0.1:8787"}
	b, err := Build(t.Context(), cfg)
	need(t, err)
	defer b.Close()
	_, _ = ownerSession(t, b)
	request := func(method, path, body string, cookies []*http.Cookie, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, cfg.HTTP.PublicURL+path, strings.NewReader(body))
		r.RemoteAddr = "192.168.1.21:54321"
		r.Header.Set("Origin", cfg.HTTP.PublicURL)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		b.HTTP.Handler().ServeHTTP(w, r)
		return w
	}
	w := request("GET", "/", "", nil, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "<!doctype html>") {
		t.Fatalf("embedded page: %d %s", w.Code, w.Body.String())
	}
	w = request("GET", "/api/auth/csrf", "", nil, "")
	var proof struct {
		Token string `json:"csrf_token"`
	}
	need(t, json.Unmarshal(w.Body.Bytes(), &proof))
	w = request("POST", "/api/auth/login", `{"username":"owner","password":"correct-horse-battery-staple-2026!"}`, w.Result().Cookies(), proof.Token)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	need(t, json.Unmarshal(w.Body.Bytes(), &proof))
	found := false
	for _, c := range cookies {
		if c.Name == "molr_lan_session_p8787" {
			found = true
			if c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
				t.Fatal(c)
			}
		}
	}
	if !found {
		t.Fatal("missing port-scoped LAN cookie")
	}
	if w.Header().Get("Strict-Transport-Security") != "" {
		t.Fatal("HSTS on HTTP")
	}
	if w = request("GET", "/api/status", "", cookies, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w = request("POST", "/api/auth/logout", `{}`, cookies, ""); w.Code != 403 {
		t.Fatal("CSRF bypass", w.Code)
	}
	if w = request("GET", "/api/not-real", "", cookies, ""); w.Code != 404 || strings.Contains(w.Body.String(), "<!doctype") {
		t.Fatal("API fell through to SPA")
	}
	for _, tc := range []struct{ host, peer, origin, forward string }{
		{"evil.example:8787", "192.168.1.21:1234", "", ""},
		{"192.168.1.20:8787", "8.8.8.8:1234", "", ""},
		{"192.168.1.20:8787", "192.168.1.21:1234", "http://evil.example", ""},
		{"192.168.1.20:8787", "192.168.1.21:1234", "", "https"},
	} {
		r := httptest.NewRequest("GET", cfg.HTTP.PublicURL+"/api/auth/csrf", nil)
		r.Host = tc.host
		r.RemoteAddr = tc.peer
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("X-Forwarded-Proto", tc.forward)
		w := httptest.NewRecorder()
		b.HTTP.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("boundary accepted: %+v %d", tc, w.Code)
		}
	}
}

func TestSetupAtomicCompletionAndConcurrentRetry(t *testing.T) {
	b, err := Build(t.Context(), testConfig(t))
	need(t, err)
	defer b.Close()
	_, err = b.InitializeSetup(t.Context(), auth.InitializeRequest{Username: "owner", Password: core.NewSecret("setup-test-password-2026!")})
	need(t, err)
	login, err := b.Services.Auth.Login(t.Context(), auth.LoginRequest{Username: "owner", Password: core.NewSecret("setup-test-password-2026!")})
	need(t, err)
	p, err := b.Services.Auth.Authenticate(t.Context(), auth.Authentication{Session: login.Session.Token})
	need(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"chat","max_model_len":32768}]}`))
	}))
	defer server.Close()
	connected, err := b.Services.Resources.Connect(t.Context(), p, resource.ConnectRequest{Name: "Local", Kind: resource.Model, Provider: "openai-compatible", Mutation: core.Mutation{IdempotencyKey: "provider"}, Binding: resource.Binding{Endpoint: server.URL + "/v1/chat/completions", AllowedOrigins: []string{server.URL}}})
	need(t, err)
	_, err = b.Services.Resources.RefreshModels(t.Context(), p, resource.RefreshModelsRequest{ID: connected.ID, Mutation: core.Mutation{IdempotencyKey: "discover", ExpectedVersion: connected.Version}})
	need(t, err)
	models, err := b.Services.Models.List(t.Context(), p)
	need(t, err)
	req := SetupRequest{ModelID: models[0].ID, Language: "en", Timezone: "Asia/Shanghai", Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD", Micros: 1_000_000}, Period: "daily"}}
	raw, _ := json.Marshal(req)
	if _, err = b.setup(t.Context(), p, raw); err == nil {
		t.Fatal("unknown price silently free")
	}
	list, err := b.Services.Assistants.List(t.Context(), p, assistant.Query{})
	need(t, err)
	if len(list.Items) != 0 {
		t.Fatal("failed setup left assistant")
	}
	req.Pricing = &resource.ModelPricing{Currency: "USD"}
	raw, _ = json.Marshal(req)
	type outcome struct {
		v   any
		err error
	}
	results := make(chan outcome, 4)
	for range 4 {
		go func() { v, e := b.setup(t.Context(), p, raw); results <- outcome{v, e} }()
	}
	var id core.ID
	for range 4 {
		r := <-results
		need(t, r.err)
		s := r.v.(SetupState)
		if s.Required || s.AssistantID == "" {
			t.Fatal(s)
		}
		if id != "" && id != s.AssistantID {
			t.Fatal("duplicate default assistant")
		}
		id = s.AssistantID
	}
	list, err = b.Services.Assistants.List(t.Context(), p, assistant.Query{})
	need(t, err)
	if len(list.Items) != 1 || list.Items[0].Name != "Molr" {
		t.Fatal(list)
	}
	events, err := persistence.Scan[event.AssistantEvent](t.Context(), b.store, "agent.events")
	need(t, err)
	// The assistant service's born contract additionally verifies delivery semantics.
	births := 0
	for _, e := range events {
		if e.Kind == event.Born {
			births++
		}
	}
	if births != 1 {
		t.Fatal("duplicate greeting event")
	}
}

func TestProbeLocalProviderOnlyReadsMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, body, state string
		code              int
	}{
		{"models", `{"data":[{"id":"chat"}]}`, "ready", 200},
		{"empty", `{"data":[]}`, "empty", 200},
		{"auth", `{"error":"unauthorized"}`, "authentication_required", 401},
		{"non-model auth", `{"code":"unauthenticated"}`, "", 401},
		{"html", `<html>hello</html>`, "", 200},
		{"wrong shape", `{"status":"ok"}`, "", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got, want := r.UserAgent(), "molr/"+buildinfo.Version; got != want {
					t.Errorf("User-Agent = %q, want %q", got, want)
				}
				if r.Method != "GET" {
					t.Error("unexpected inference")
				}
				if r.URL.Path == "/api/tags" || r.URL.Path == "/api/v0/models" {
					w.WriteHeader(404)
					return
				}
				if r.URL.Path != "/v1/models" {
					t.Error(r.URL.Path)
				}
				w.WriteHeader(tc.code)
				w.Write([]byte(tc.body))
			}))
			defer server.Close()
			result := probeLocalProvider(t.Context(), server.Client(), server.URL)
			if tc.state == "" {
				if result != nil {
					t.Fatal(result)
				}
			} else if result == nil || result.State != tc.state || result.Preset != "custom" {
				t.Fatal(result)
			}
		})
	}
}
