package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/pricing"
)

func TestPricingRefreshHTTPAuthorizationAndRestartCache(t *testing.T) {
	cfg := testConfig(t)
	cfg.HTTP.TLSMode, cfg.HTTP.TrustedProxies = config.LocalHTTP, nil
	cfg.HTTP.Listen, cfg.HTTP.PublicURL = "127.0.0.1:18599", "http://127.0.0.1:18599"
	cfg.Pricing.RulesFile = filepath.Join(t.TempDir(), "rules.json")
	data, err := os.ReadFile("../pricing/providers.json")
	need(t, err)
	need(t, os.WriteFile(cfg.Pricing.RulesFile, data, 0600))
	b, err := Build(t.Context(), cfg)
	need(t, err)
	t.Cleanup(func() { _ = b.Close() })
	admin, _ := ownerSession(t, b)
	manager := b.Services.Auth.(*auth.Manager)
	_, err = manager.CreateUser(t.Context(), admin, auth.CreateUserRequest{Username: "member", GroupID: auth.DefaultGroupID, Password: core.NewSecret("member-price-test-password"), Mutation: core.Mutation{IdempotencyKey: "pricing-member"}})
	need(t, err)
	request := func(method, path, body string, cookies []*http.Cookie, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, cfg.HTTP.PublicURL+path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Origin", cfg.HTTP.PublicURL)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		for _, cookie := range cookies {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		b.HTTP.Handler().ServeHTTP(w, r)
		return w
	}
	login := func(username, password string) ([]*http.Cookie, string) {
		w := request("GET", "/api/auth/csrf", "", nil, "")
		var proof struct {
			Token string `json:"csrf_token"`
		}
		need(t, json.Unmarshal(w.Body.Bytes(), &proof))
		body, _ := json.Marshal(map[string]string{"username": username, "password": password})
		w = request("POST", "/api/auth/login", string(body), w.Result().Cookies(), proof.Token)
		if w.Code != 200 {
			t.Fatal("login", w.Code, w.Body.String())
		}
		need(t, json.Unmarshal(w.Body.Bytes(), &proof))
		return w.Result().Cookies(), proof.Token
	}
	const endpoint = "/api/models/refresh-pricing"
	if w := request("POST", endpoint, `{}`, nil, ""); w.Code != 401 {
		t.Fatal("anonymous", w.Code)
	}
	cookies, csrf := login("owner", "correct-horse-battery-staple-2026!")
	if w := request("POST", endpoint, `{"rules_url":"https://unapproved.example/prices.json"}`, cookies, csrf); w.Code != 400 {
		t.Fatal("source parameter accepted", w.Code)
	}
	if w := request("POST", endpoint, `{}`, cookies, ""); w.Code != 403 {
		t.Fatal("CSRF bypass", w.Code)
	}
	memberCookies, memberCSRF := login("member", "member-price-test-password")
	if w := request("POST", endpoint, `{}`, memberCookies, memberCSRF); w.Code != 403 {
		t.Fatal("member changed global prices", w.Code)
	}
	old, _ := b.Pricing.Lookup("deepseek", "https://api.deepseek.com", "deepseek-flash", time.Now())
	changed := bytes.Replace(data, []byte(`"version": "2026-10-23"`), []byte(`"version": "manual-refresh"`), 1)
	need(t, os.WriteFile(cfg.Pricing.RulesFile, changed, 0600))
	w := request("POST", endpoint, `{}`, cookies, csrf)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result pricing.RefreshResult
	need(t, json.Unmarshal(w.Body.Bytes(), &result))
	if !result.Updated || result.Version != "manual-refresh" || result.Source != "file" {
		t.Fatal(result)
	}
	q, _ := b.Pricing.Lookup("deepseek", "https://api.deepseek.com", "deepseek-flash", time.Now())
	if q.Version != "manual-refresh" || old.Version == q.Version {
		t.Fatal("live update or snapshot failed")
	}
	need(t, os.WriteFile(cfg.Pricing.RulesFile, []byte(`{}`), 0600))
	if w = request("POST", endpoint, `{}`, cookies, csrf); w.Code != 409 || !strings.Contains(w.Body.String(), "pricing_rules_invalid") {
		t.Fatal("invalid refresh", w.Code, w.Body.String())
	}
	need(t, b.Close())
	b, err = Build(t.Context(), cfg)
	need(t, err)
	q, _ = b.Pricing.Lookup("deepseek", "https://api.deepseek.com", "deepseek-flash", time.Now())
	if q.Version != "manual-refresh" {
		t.Fatal("restart lost cached tariff", q)
	}
}
