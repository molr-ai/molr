package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/oauthbroker"
)

type oauthFixtureProvider struct{ calls int }

func (p *oauthFixtureProvider) AuthorizationURL(_ context.Context, _ core.Principal, a oauthbroker.Authorization) (string, error) {
	return "https://provider.example/authorize?" + url.Values{"state": {a.State}, "nonce": {a.Nonce}, "redirect_uri": {a.RedirectURI}, "code_challenge": {a.Challenge}, "code_challenge_method": {"S256"}, "response_type": {"code"}}.Encode(), nil
}
func (p *oauthFixtureProvider) Complete(_ context.Context, _ core.Principal, g oauthbroker.Grant) (core.ID, error) {
	p.calls++
	return "test-connection", nil
}
func TestOAuthRoutesRequireLiveBrowserSessionAndCSRF(t *testing.T) {
	f := fixture(t)
	s := f.Server.(*HTTPServer)
	provider := &oauthFixtureProvider{}
	s.options.OAuth = oauthbroker.New(map[string]oauthbroker.Provider{"fixture": provider}, f.Services.Auth.(*auth.Manager).ValidatePrincipal)
	// Recompose routes before serving, keeping the real auth/CSRF middleware.
	s.mux = http.NewServeMux()
	s.routes()
	request := func(path string, body any, strip string) *httptest.ResponseRecorder {
		t.Helper()
		data, _ := json.Marshal(body)
		r := f.MutationRequest()
		r.URL.Path = path
		r.Body = io.NopCloser(strings.NewReader(string(data)))
		r.ContentLength = int64(len(data))
		if strip == "auth" {
			f.StripCredentials(r)
		}
		if strip == "csrf" {
			f.StripCSRF(r)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	start := oauthbroker.StartRequest{Provider: "fixture", RedirectURI: "http://127.0.0.1:1455/auth/callback"}
	for strip, want := range map[string]int{"auth": 401, "csrf": 403} {
		if w := request("/api/oauth/start", start, strip); w.Code != want {
			t.Fatalf("%s: %d", strip, w.Code)
		}
	}
	w := request("/api/oauth/start", start, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var a oauthbroker.Started
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	q := oauthbroker.CompleteRequest{ID: a.ID, State: a.State, Code: "fake-code"}
	w = request("/api/oauth/complete", q, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"Status":"connected"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request("/api/oauth/complete", q, "")
	if w.Code != 200 || provider.calls != 1 {
		t.Fatal("replay exchanged again")
	}
	f.RevokeSession()
	if w := request("/api/oauth/complete", q, ""); w.Code != 401 {
		t.Fatal("revoked session accepted", w.Code)
	}
}
