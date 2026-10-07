package api

import (
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPLoginLinkExchangeRequiresCSRFAndOnlyIssuesCookie(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	handler := f.Server.Handler()
	cookie, err := f.ReadRequest().Cookie(sessionCookie)
	testkit.RequireNoError(t, err)
	p, err := f.Services.Auth.Authenticate(ctx, auth.Authentication{Session: core.NewSecret(cookie.Value)})
	testkit.RequireNoError(t, err)
	proof, err := f.Services.Auth.Reauthenticate(ctx, p, auth.ReauthenticateRequest{Password: core.NewSecret("strong-initial-password-123!"), Purpose: "login_link.create"})
	testkit.RequireNoError(t, err)
	body, _ := json.Marshal(map[string]any{"name": "test", "expires_in": 3600, "target_path": "/settings/theme", "verification": proof.Token.Reveal()})
	create := f.MutationRequest()
	create.URL.Path = "/api/auth/login-links"
	create.Body = io.NopCloser(strings.NewReader(string(body)))
	create.ContentLength = int64(len(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, create)
	if w.Code != 200 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var issued struct {
		Value string `json:"value"`
	}
	testkit.RequireNoError(t, json.Unmarshal(w.Body.Bytes(), &issued))
	if issued.Value == "" || issued.Value == "[REDACTED]" {
		t.Fatal("missing raw token on creation")
	}
	list := f.ReadRequest()
	list.URL.Path = "/api/auth/login-links"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, list)
	if w.Code != 200 || strings.Contains(w.Body.String(), issued.Value) {
		t.Fatal("list missing or secret leaked")
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "https://molr.example/api/auth/login-links/redeem?token="+issued.Value, nil))
	if w.Code != 400 {
		t.Fatal("query token accepted")
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "https://molr.example/api/auth/csrf", nil))
	var csrf struct {
		Token string `json:"csrf_token"`
	}
	testkit.RequireNoError(t, json.Unmarshal(w.Body.Bytes(), &csrf))
	prelogin := w.Result().Cookies()[0]
	request := func() *http.Request {
		r := httptest.NewRequest("POST", "https://molr.example/api/auth/login-links/redeem", strings.NewReader(`{"token":"`+issued.Value+`","device_name":"Phone"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://molr.example")
		r.Header.Set("X-CSRF-Token", csrf.Token)
		r.AddCookie(prelogin)
		return r
	}
	for _, kind := range []string{"csrf", "origin", "cookie"} {
		r := request()
		switch kind {
		case "csrf":
			r.Header.Del("X-CSRF-Token")
		case "origin":
			r.Header.Set("Origin", "https://evil.example")
		case "cookie":
			r.Header.Del("Cookie")
		}
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("%s bypass: %d", kind, w.Code)
		}
		f.Advance(time.Minute)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, request())
	if w.Code != 200 {
		t.Fatalf("redeem: %d %s", w.Code, w.Body.String())
	}
	var session *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			session = c
		}
	}
	if session == nil || !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteStrictMode {
		t.Fatal("missing secure session cookie")
	}
	if strings.Contains(w.Body.String(), session.Value) || strings.Contains(w.Body.String(), issued.Value) {
		t.Fatal("raw credential leaked from exchange")
	}
	var result struct {
		Target string `json:"target_path"`
	}
	testkit.RequireNoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	if result.Target != "/settings/theme" {
		t.Fatal("destination lost")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cacheable exchange")
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, request())
	if w.Code != 401 {
		t.Fatal("replayed link authenticated")
	}
}

func TestHTTPLoginLinkPreviewOnlyReturnsExpiryWithoutConsuming(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	handler := f.Server.Handler()
	cookie, err := f.ReadRequest().Cookie(sessionCookie)
	testkit.RequireNoError(t, err)
	p, err := f.Services.Auth.Authenticate(ctx, auth.Authentication{Session: core.NewSecret(cookie.Value)})
	testkit.RequireNoError(t, err)
	proof, err := f.Services.Auth.Reauthenticate(ctx, p, auth.ReauthenticateRequest{Password: core.NewSecret("strong-initial-password-123!"), Purpose: "login_link.create"})
	testkit.RequireNoError(t, err)
	link, err := f.Services.Auth.CreateLoginLink(ctx, p, auth.CreateLoginLinkRequest{Name: "preview", TargetPath: "/settings/theme", ExpiresIn: time.Hour, Verification: proof})
	testkit.RequireNoError(t, err)
	request := func() *http.Request {
		f.Advance(time.Minute)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "https://molr.example/api/auth/csrf", nil))
		var csrf struct {
			Token string `json:"csrf_token"`
		}
		testkit.RequireNoError(t, json.Unmarshal(w.Body.Bytes(), &csrf))
		r := httptest.NewRequest("POST", "https://molr.example/api/auth/login-links/preview", strings.NewReader(`{"token":"`+link.Value.Reveal()+`"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://molr.example")
		r.Header.Set("X-CSRF-Token", csrf.Token)
		r.AddCookie(w.Result().Cookies()[0])
		return r
	}
	for _, missing := range []string{"X-CSRF-Token", "Origin", "Cookie"} {
		r := request()
		r.Header.Del(missing)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("preview without %s: %d", missing, w.Code)
		}
	}
	for range 2 {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, request())
		if w.Code != 200 {
			t.Fatalf("preview: %d %s", w.Code, w.Body.String())
		}
		var result map[string]time.Time
		testkit.RequireNoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		if len(result) != 1 || !result["expires_at"].Equal(link.Link.ExpiresAt) {
			t.Fatal("preview must return only the real expiry")
		}
		if len(w.Result().Cookies()) != 0 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("preview issued cookie or allowed caching")
		}
	}
	r := request()
	r.URL.Path = "/api/auth/login-links/redeem"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("preview consumed link: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, request())
	if w.Code != 401 {
		t.Fatal("used link preview succeeded")
	}
}
