package shellsession

import (
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestForwardStripsAllMolrCredentialsPreservesApplicationCookies(t *testing.T) {
	h := http.Header{"Cookie": {"molr_dev_session_p5191=secret; __Host-molr_session=secret2; molr_lan_csrf_p8080=proof; application_session=app"}, "Authorization": {"Bearer secret"}, "X-Csrf-Token": {"proof"}, "X-Forwarded-For": {"spoof"}}
	stripForwardCredentials(h)
	if h.Get("Cookie") != "application_session=app" || h.Get("Authorization") != "" || h.Get("X-CSRF-Token") != "" || h.Get("X-Forwarded-For") != "" {
		t.Fatal(h)
	}
}
func TestForwardRequiresSessionAndTaskOwnership(t *testing.T) {
	b, p, e := taskFixture(t)
	s := b.shellTasks
	task := Task{OwnerID: p.OwnerID, AssistantID: e.AssistantID}
	req := httptest.NewRequest("GET", "http://127.0.0.1:12345/", nil)
	if _, err := s.forwardPrincipal(t.Context(), req, task); core.CodeOf(err) != core.Unauthenticated {
		t.Fatal(err)
	}
	login, err := b.Services.Auth.Login(t.Context(), auth.LoginRequest{Username: "owner", Password: core.NewSecret("correct-horse-battery-staple-2026!"), Client: auth.Client{Address: "127.0.0.1", DeviceName: "forward"}})
	need(t, err)
	req.AddCookie(&http.Cookie{Name: "molr_dev_session_p5191", Value: login.Session.Token.Reveal()})
	_, err = s.forwardPrincipal(t.Context(), req, task)
	need(t, err)
	task.OwnerID = "foreign"
	if _, err = s.forwardPrincipal(t.Context(), req, task); core.CodeOf(err) != core.Forbidden {
		t.Fatal("foreign task accepted", err)
	}
	task.OwnerID = p.OwnerID
	second, err := b.Services.Auth.Login(t.Context(), auth.LoginRequest{Username: "owner", Password: core.NewSecret("correct-horse-battery-staple-2026!"), Client: auth.Client{Address: "127.0.0.1", DeviceName: "other-entry"}})
	need(t, err)
	req.AddCookie(&http.Cookie{Name: "molr_dev_session_p8080", Value: second.Session.Token.Reveal()})
	if _, err = s.forwardPrincipal(t.Context(), req, task); core.CodeOf(err) != core.Forbidden {
		t.Fatal("ambiguous login accepted", err)
	}
	req.URL.RawQuery = "__molr_login_port=5191"
	_, err = s.forwardPrincipal(t.Context(), req, task)
	need(t, err)
	req.AddCookie(&http.Cookie{Name: forwardSelectorCookie(req), Value: "molr_dev_session_p5191"})
	req.URL.RawQuery = ""
	principal, err := b.Services.Auth.Authenticate(t.Context(), auth.Authentication{Session: login.Session.Token})
	need(t, err)
	need(t, b.Services.Auth.Logout(t.Context(), principal))
	if _, err = s.forwardPrincipal(t.Context(), req, task); core.CodeOf(err) != core.Unauthenticated {
		t.Fatal("revoked session accepted", err)
	}
}
