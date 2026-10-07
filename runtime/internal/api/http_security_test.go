package api

import (
	"encoding/json"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/tests/testkit"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPLoginCookiesAndOneTimeTokenResponse(t *testing.T) {
	f := fixture(t)
	handler := f.Server.Handler()
	csrfRequest := httptest.NewRequest("GET", "https://molr.example/api/auth/csrf", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, csrfRequest)
	if w.Code != 200 {
		t.Fatalf("CSRF status=%d: %s", w.Code, w.Body.String())
	}
	var csrf struct {
		Token string `json:"csrf_token"`
	}
	testkit.RequireNoError(t, json.Unmarshal(w.Body.Bytes(), &csrf))
	var prelogin *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == loginCookie {
			prelogin = cookie
		}
	}
	if prelogin == nil || !prelogin.Secure || !prelogin.HttpOnly || csrf.Token == "" || csrf.Token == prelogin.Value {
		t.Fatal("pre-login CSRF cookie/token invalid")
	}
	login := httptest.NewRequest("POST", "https://molr.example/api/auth/login", strings.NewReader(`{"username":"owner","password":"strong-initial-password-123!"}`))
	login.Header.Set("Content-Type", "application/json")
	login.Header.Set("Origin", "https://molr.example")
	login.Header.Set("X-CSRF-Token", csrf.Token)
	login.AddCookie(prelogin)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, login)
	if w.Code != 200 {
		t.Fatalf("login status=%d: %s", w.Code, w.Body.String())
	}
	var session *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == sessionCookie {
			session = cookie
		}
	}
	if session == nil || !session.Secure || !session.HttpOnly || session.Path != "/" || session.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe session cookie")
	}
	if strings.Contains(w.Body.String(), session.Value) {
		t.Fatal("raw browser session leaked in JSON")
	}
	testkit.RequireNoError(t, json.Unmarshal(w.Body.Bytes(), &csrf))
	if csrf.Token == "" || csrf.Token == session.Value {
		t.Fatal("session CSRF token missing or reused")
	}
	var browserProof *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == csrfCookie {
			browserProof = cookie
		}
	}
	if browserProof == nil || browserProof.HttpOnly || !browserProof.Secure || browserProof.Path != "/" || browserProof.Domain != "" || browserProof.SameSite != http.SameSiteStrictMode || browserProof.Value != csrf.Token || browserProof.Value == session.Value {
		t.Fatal("browser CSRF proof must be readable, session-bound and separate from credentials")
	}
	request := func(method, path, body string) *http.Request {
		r := httptest.NewRequest(method, "https://molr.example"+path, strings.NewReader(body))
		r.AddCookie(session)
		r.Header.Set("Origin", "https://molr.example")
		r.Header.Set("X-CSRF-Token", csrf.Token)
		r.Header.Set("Content-Type", "application/json")
		return r
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, request("POST", "/api/auth/reauthenticate", `{"password":"strong-initial-password-123!","purpose":"token.create"}`))
	if w.Code != 200 {
		t.Fatalf("reauth status=%d: %s", w.Code, w.Body.String())
	}
	var verification struct {
		Token string `json:"verification"`
	}
	testkit.RequireNoError(t, json.Unmarshal(w.Body.Bytes(), &verification))
	if verification.Token == "" || verification.Token == "[REDACTED]" {
		t.Fatal("verification DTO lost secret")
	}
	body, _ := json.Marshal(map[string]any{"name": "client", "scopes": []string{"read"}, "expires_at": time.Now().Add(time.Hour), "verification": verification.Token})
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, request("POST", "/api/auth/tokens", string(body)))
	if w.Code != 200 {
		t.Fatalf("token status=%d: %s", w.Code, w.Body.String())
	}
	var issued struct {
		Value string `json:"value"`
	}
	testkit.RequireNoError(t, json.Unmarshal(w.Body.Bytes(), &issued))
	if issued.Value == "" || issued.Value == "[REDACTED]" {
		t.Fatal("token not delivered exactly once")
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, request("GET", "/api/auth/tokens", ""))
	if w.Code != 200 || strings.Contains(w.Body.String(), issued.Value) {
		t.Fatal("token metadata endpoint disclosed secret")
	}
	bearer := httptest.NewRequest("GET", "https://molr.example/api/status", nil)
	bearer.Header.Set("Authorization", "Bearer "+issued.Value)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, bearer)
	if w.Code != 200 {
		t.Fatalf("issued bearer unusable: %d", w.Code)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, request("POST", "/api/auth/tokens", string(body)))
	if w.Code == 200 {
		t.Fatal("single-use verification created another token")
	}
}
func TestHTTPRejectsSecretObjectsAndInternalCapabilityRoutes(t *testing.T) {
	f := fixture(t)
	handler := f.Server.Handler()
	for _, path := range []string{"/api/auth/initialize", "/api/auth/recover", "/api/conversations/publish", "/api/operations/begin", "/api/usage/reserve", "/api/agent/checkpoints"} {
		r := f.MutationRequest()
		r.URL.Path = path
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("internal capability route %s exposed: %d", path, w.Code)
		}
	}
	r := f.MutationRequest()
	r.URL.Path = "/api/auth/reauthenticate"
	r.Body = http.NoBody
	r = httptest.NewRequest("POST", r.URL.String(), strings.NewReader(`{"password":{"value":"secret"},"purpose":"token.create"}`))
	authorized := f.MutationRequest()
	r.Header = authorized.Header.Clone()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("Secret object decoded instead of explicit string DTO: %d", w.Code)
	}
	r = f.ReadRequest()
	r.URL.RawQuery = "access_token=secret"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 400 || strings.Contains(w.Body.String(), "secret") {
		t.Fatal("query credential accepted or echoed")
	}
}
func TestHTTPTransportChecksTLSHostAndTrustedProxyChain(t *testing.T) {
	f := fixture(t)
	server := f.Server.(*HTTPServer)
	r := f.ReadRequest()
	r.Host = "evil.example"
	if _, err := server.transport(r); err == nil {
		t.Fatal("host injection accepted")
	}
	r = f.ReadRequest()
	r.TLS = nil
	if _, err := server.transport(r); err == nil {
		t.Fatal("plaintext accepted")
	}
	proxy := &HTTPServer{config: server.config, origin: server.origin}
	proxy.config.HTTP.TLSMode = config.TrustedProxy
	proxy.config.HTTP.TrustedProxies = []string{"192.0.2.0/24"}
	r = f.ReadRequest()
	r.RemoteAddr = "192.0.2.10:12345"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 198.51.100.8, 192.0.2.11")
	address, err := proxy.transport(r)
	testkit.RequireNoError(t, err)
	if address != "198.51.100.8" {
		t.Fatalf("untrusted forwarded prefix selected: %s", address)
	}
	r.RemoteAddr = "198.51.100.8:12345"
	if _, err = proxy.transport(r); err == nil {
		t.Fatal("untrusted peer spoofed HTTPS headers")
	}
}
