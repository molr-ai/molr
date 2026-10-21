package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
)

func TestCachedUsageRechecksIdentityGroupAndAdministrator(t *testing.T) {
	f := newMultiUserFixture(t)
	m := f.b.Services.Auth.(*auth.Manager)
	get := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "https://runtime.example"+path, nil)
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("X-Forwarded-Proto", "https")
		r.AddCookie(&http.Cookie{Name: "__Host-molr_session", Value: f.aliceSession.Token.Reveal()})
		w := httptest.NewRecorder()
		f.b.HTTP.Handler().ServeHTTP(w, r)
		return w
	}
	first := get("/api/account/quotas")
	hit := get("/api/account/quotas")
	if first.Code != 200 || first.Header().Get("X-Cache") != "MISS" || hit.Header().Get("X-Cache") != "HIT" {
		t.Fatal(first.Code, first.Body, hit.Header())
	}
	g := f.group
	g.Name = "Changed group"
	_, err := m.SaveGroup(t.Context(), f.admin, auth.SaveGroupRequest{Mutation: core.Mutation{IdempotencyKey: "change-cache-group", ExpectedVersion: g.Version}, Group: g})
	need(t, err)
	if w := get("/api/account/quotas"); w.Code != 200 || w.Header().Get("X-Cache") != "MISS" {
		t.Fatal(w.Code, w.Header())
	}
	u, err := m.CurrentUser(t.Context(), f.alice)
	need(t, err)
	updated, err := m.UpdateUser(t.Context(), f.admin, auth.UpdateUserRequest{ID: u.ID, DisplayName: u.DisplayName, GroupID: auth.AdminGroupID, Mutation: core.Mutation{IdempotencyKey: "promote-cache-user", ExpectedVersion: u.Version}})
	need(t, err)
	login := func() {
		issued, err := m.Login(t.Context(), auth.LoginRequest{Username: "alice", Password: core.NewSecret("alice-password-2026!"), Client: auth.Client{Address: "alice"}})
		need(t, err)
		f.aliceSession = *issued.Session
		f.alice, err = m.Authenticate(t.Context(), auth.Authentication{Session: issued.Session.Token})
		need(t, err)
	}
	login()
	first = get("/api/admin/usage")
	hit = get("/api/admin/usage")
	if first.Code != 200 || hit.Header().Get("X-Cache") != "HIT" {
		t.Fatal(first.Code, first.Body, hit.Header())
	}
	_, err = m.UpdateUser(t.Context(), f.admin, auth.UpdateUserRequest{ID: u.ID, DisplayName: u.DisplayName, GroupID: f.group.ID, Mutation: core.Mutation{IdempotencyKey: "demote-cache-user", ExpectedVersion: updated.Version}})
	need(t, err)
	if w := get("/api/admin/usage"); w.Code != 401 || w.Header().Get("X-Cache") != "" {
		t.Fatal("changed membership retained session", w.Code)
	}
	login()
	if w := get("/api/admin/usage"); w.Code != 403 || w.Header().Get("X-Cache") != "" {
		t.Fatal(w.Code, w.Header())
	}
	need(t, m.Logout(t.Context(), f.alice))
	if w := get("/api/account/quotas"); w.Code != 401 || w.Header().Get("X-Cache") != "" {
		t.Fatal(w.Code, w.Header())
	}
}

func TestCachedSandboxVisibilityChangesWithGroup(t *testing.T) {
	f := newMultiUserFixture(t)
	m := f.b.Services.Auth.(*auth.Manager)
	get := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "https://runtime.example/api/sandboxes", nil)
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("X-Forwarded-Proto", "https")
		r.AddCookie(&http.Cookie{Name: "__Host-molr_session", Value: f.aliceSession.Token.Reveal()})
		w := httptest.NewRecorder()
		f.b.HTTP.Handler().ServeHTTP(w, r)
		return w
	}
	first := get()
	hit := get()
	if first.Code != 200 || hit.Header().Get("X-Cache") != "HIT" {
		t.Fatal(first.Code, first.Body, hit.Header())
	}
	g := f.group
	g.Resources = append(g.Resources, auth.ResourceReference{Kind: "sandbox", ID: "local"})
	_, err := m.SaveGroup(t.Context(), f.admin, auth.SaveGroupRequest{Mutation: core.Mutation{IdempotencyKey: "sandbox-cache-visibility", ExpectedVersion: g.Version}, Group: g})
	need(t, err)
	fresh := get()
	if fresh.Code != 200 || fresh.Header().Get("X-Cache") != "MISS" || fresh.Body.String() == first.Body.String() {
		t.Fatal(fresh.Code, fresh.Header(), fresh.Body)
	}
}
