package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPDomainAuthorizationPrecedesDeferredRequestDecoding(t *testing.T) {
	f := newMultiUserFixture(t)
	own := f.assistant(t, f.alice, "owned-terminal")
	foreign := f.assistant(t, f.bob, "foreign-terminal")
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://runtime.example"+path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-Host", "runtime.example")
		r.Header.Set("Origin", "https://runtime.example")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", f.aliceSession.CSRF.Reveal())
		r.AddCookie(&http.Cookie{Name: "__Host-molr_session", Value: f.aliceSession.Token.Reveal()})
		response := httptest.NewRecorder()
		f.b.HTTP.Handler().ServeHTTP(response, r)
		return response
	}
	for _, id := range []string{string(foreign.ID), "missing-assistant"} {
		path := "/api/assistants/" + id + "/terminals"
		baseline := request("POST", path, `{"request_id":"request","client_id":"window","compute_id":"compute"}`)
		if baseline.Code != http.StatusForbidden && baseline.Code != http.StatusNotFound {
			t.Fatalf("foreign scope: %d %s", baseline.Code, baseline.Body)
		}
		for _, body := range []string{`{"unexpected":true}`, `{"client_id":"` + strings.Repeat("x", 4096) + `"}`} {
			response := request("POST", path, body)
			if response.Code != baseline.Code {
				t.Fatalf("request shape masked object authorization: %d %s; want %d", response.Code, response.Body, baseline.Code)
			}
		}
	}
	for _, body := range []string{`{"Unexpected":true}`, `{"Server":"` + strings.Repeat("x", 16<<10) + `"}`} {
		response := request("PUT", "/api/components/browser/settings", body)
		if response.Code != http.StatusForbidden {
			t.Fatalf("request shape masked administrator check: %d %s", response.Code, response.Body)
		}
	}
	response := request("POST", "/api/assistants/"+string(own.ID)+"/terminals", `{"unexpected":true}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("authorized unknown fields accepted: %d %s", response.Code, response.Body)
	}
}
