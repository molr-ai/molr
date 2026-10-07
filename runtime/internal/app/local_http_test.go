package app

import (
	"encoding/json"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalHTTPBrowserLoginAndAccessBoundaries(t *testing.T) {
	cfg := config.Defaults(t.TempDir())
	cfg.HTTP.TLSMode = config.TLSMode("local")
	cfg.HTTP.Listen = "127.0.0.1:8080"
	cfg.HTTP.PublicURL = "http://127.0.0.1:8080"
	b, err := Build(t.Context(), cfg)
	need(t, err)
	defer b.Close()
	_, err = b.Bootstrap.Initialize(t.Context(), auth.InitializeRequest{Username: "owner", Password: core.NewSecret("local-test-password-2026!")})
	need(t, err)
	server := httptest.NewServer(b.HTTP.Handler())
	defer server.Close()
	jar, err := cookiejar.New(nil)
	need(t, err)
	client := server.Client()
	client.Jar = jar
	request := func(method, path, body, csrf string) *http.Response {
		t.Helper()
		r, err := http.NewRequestWithContext(t.Context(), method, server.URL+path, strings.NewReader(body))
		need(t, err)
		r.Host = "127.0.0.1:8080"
		r.Header.Set("Origin", cfg.HTTP.PublicURL)
		r.Header.Set("Content-Type", "application/json")
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		response, err := client.Do(r)
		need(t, err)
		t.Cleanup(func() { response.Body.Close() })
		return response
	}
	if r := request("GET", "/api/status", "", ""); r.StatusCode != 401 {
		t.Fatal("runtime information accepted an unauthenticated request", r.StatusCode)
	}
	response := request("GET", "/api/auth/csrf", "", "")
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	if response.Header.Get("Strict-Transport-Security") != "" {
		t.Fatal("HTTP dev mode sets HSTS")
	}
	var proof struct {
		Token string `json:"csrf_token"`
	}
	need(t, json.NewDecoder(response.Body).Decode(&proof))
	if proof.Token == "" {
		t.Fatal("missing CSRF")
	}
	response = request("POST", "/api/auth/login", `{"username":"owner","password":"local-test-password-2026!"}`, proof.Token)
	if response.StatusCode != 200 {
		t.Fatal("browser cookie login failed", response.StatusCode)
	}
	found := false
	for _, cookie := range response.Cookies() {
		if cookie.MaxAge < 0 {
			continue
		}
		if cookie.Name == "molr_dev_csrf" {
			if cookie.Secure || cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Value == "" {
				t.Fatal("incorrect local CSRF cookie attributes")
			}
			continue
		}
		found = true
		if cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || strings.HasPrefix(cookie.Name, "__Host-") {
			t.Fatal("incorrect local cookie attributes")
		}
	}
	if !found {
		t.Fatal("no session cookie")
	}
	need(t, json.NewDecoder(response.Body).Decode(&proof))
	if r := request("GET", "/api/auth/me", "", ""); r.StatusCode != 200 {
		t.Fatal("HTTP cookie not sent by browser jar", r.StatusCode)
	}
	response = request("GET", "/api/status", "", "")
	if response.StatusCode != 200 {
		t.Fatal("runtime information unavailable", response.StatusCode)
	}
	var runtimeInfo map[string]any
	need(t, json.NewDecoder(response.Body).Decode(&runtimeInfo))
	if runtimeInfo["name"] != "Molr Runtime" || runtimeInfo["status"] != "running" || runtimeInfo["public_url"] != cfg.HTTP.PublicURL {
		t.Fatalf("runtime information differs from configured service: %#v", runtimeInfo)
	}
	if r := request("POST", "/api/auth/logout", `{}`, ""); r.StatusCode != 403 {
		t.Fatal("CSRF was bypassed", r.StatusCode)
	}
	for _, tc := range []struct {
		name, host, peer, origin string
		forwarded                bool
	}{
		{"nonlocal peer", "127.0.0.1:8080", "192.168.1.5:1234", cfg.HTTP.PublicURL, false},
		{"DNS rebinding", "attacker.example:8080", "127.0.0.1:1234", "", false},
		{"remote origin", "127.0.0.1:8080", "127.0.0.1:1234", "http://attacker.example", false},
		{"forwarded request", "127.0.0.1:8080", "127.0.0.1:1234", cfg.HTTP.PublicURL, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", cfg.HTTP.PublicURL+"/api/auth/csrf", nil)
			r.Host = tc.host
			r.RemoteAddr = tc.peer
			r.Header.Set("Origin", tc.origin)
			if tc.forwarded {
				r.Header.Set("X-Forwarded-For", "127.0.0.1")
			}
			w := httptest.NewRecorder()
			b.HTTP.Handler().ServeHTTP(w, r)
			if w.Code != 403 {
				t.Fatal("request boundary bypassed", w.Code)
			}
		})
	}
	if r := request("POST", "/api/auth/logout", `{}`, proof.Token); r.StatusCode != 200 {
		t.Fatal("logout failed", r.StatusCode)
	}
	if r := request("GET", "/api/auth/me", "", ""); r.StatusCode != 401 {
		t.Fatal("logout left session active", r.StatusCode)
	}
}
