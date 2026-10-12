package app

import (
	"encoding/json"
	"molr.ai/runtime/internal/config"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMaintenanceAPIRequiresAuthenticationCSRFAndKnownRequests(t *testing.T) {
	cfg := testConfig(t)
	cfg.HTTP.TLSMode = config.LocalHTTP
	cfg.HTTP.TrustedProxies = nil
	cfg.HTTP.Listen = "127.0.0.1:18599"
	cfg.HTTP.PublicURL = "http://127.0.0.1:18599"
	b, err := Build(t.Context(), cfg)
	need(t, err)
	defer b.Close()
	ownerSession(t, b)
	request := func(method, path, body string, cookies []*http.Cookie, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, cfg.HTTP.PublicURL+path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1234"
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
	if w := request("GET", "/api/components", "", nil, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := request("GET", "/api/auth/csrf", "", nil, "")
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
	for _, path := range []string{"/api/components/browser/install", "/api/components/browser/update", "/api/components/browser/repair", "/api/updates/apply", "/api/maintenance/tasks/test/cancel"} {
		if w = request("POST", path, `{}`, cookies, ""); w.Code != 403 {
			t.Fatal("CSRF bypass", path, w.Code)
		}
	}
	if w = request("GET", "/api/components", "", cookies, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "maintenance_not_installed") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = request("POST", "/api/components/browser/install", `{"idempotency_key":"01234567890123456789012345678901","url":"https://attacker.invalid/program"}`, cookies, proof.Token); w.Code != 400 {
		t.Fatal("arbitrary download accepted", w.Code, w.Body.String())
	}
	if w = request("POST", "/api/updates/apply", `{"idempotency_key":"01234567890123456789012345678901","version":"999.0.0"}`, cookies, proof.Token); w.Code != 400 {
		t.Fatal("unchecked update accepted", w.Code, w.Body.String())
	}
}
