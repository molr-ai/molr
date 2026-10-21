package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/resultcache"
)

func cacheServer(t *testing.T) *HTTPServer {
	t.Helper()
	c := resultcache.New[string, []byte](resultcache.Options[[]byte]{MaxEntries: 8, MaxCost: 1 << 20, LoadTimeout: time.Second, Cost: func(v []byte) int64 { return int64(len(v)) }})
	t.Cleanup(c.Close)
	return &HTTPServer{mux: http.NewServeMux(), responses: c, options: Options{ResponseCacheScope: func(context.Context, core.Principal, string) (string, error) { return "scope", nil }}}
}
func cacheRequest(s *HTTPServer, path, user string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	p := core.Principal{OwnerID: core.ID(user), SubjectID: core.ID(user), Kind: core.ActorOwner, SessionID: "session"}
	r = r.WithContext(context.WithValue(r.Context(), principalKey{}, p))
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	return w
}
func TestResponseCacheKeysRefreshMetadataAndScope(t *testing.T) {
	s := cacheServer(t)
	const route = "GET /api/account/usage"
	calls := 0
	s.handle(route, func(r *http.Request) (any, error) {
		calls++
		if r.URL.Query().Has("refresh") {
			t.Error("refresh control leaked to service")
		}
		return map[string]any{"calls": calls}, nil
	})
	first := cacheRequest(s, "/api/account/usage?a=1&b=2", "alice")
	if first.Code != 200 || first.Header().Get("X-Cache") != "MISS" || first.Header().Get("X-Cache-Written-At") == "" {
		t.Fatal(first.Code, first.Header(), first.Body)
	}
	hit := cacheRequest(s, "/api/account/usage?b=2&a=1", "alice")
	if calls != 1 || hit.Header().Get("X-Cache") != "HIT" || hit.Body.String() != first.Body.String() || hit.Header().Get("X-Cache-Written-At") != first.Header().Get("X-Cache-Written-At") {
		t.Fatal(calls, hit.Header())
	}
	other := cacheRequest(s, "/api/account/usage?a=1&b=2", "bob")
	if calls != 2 || other.Header().Get("X-Cache") != "MISS" {
		t.Fatal(calls)
	}
	changed := cacheRequest(s, "/api/account/usage?a=2&b=2", "alice")
	if calls != 3 || changed.Header().Get("X-Cache") != "MISS" {
		t.Fatal(calls)
	}
	refresh := cacheRequest(s, "/api/account/usage?a=1&b=2&refresh=true", "alice")
	hit = cacheRequest(s, "/api/account/usage?a=1&b=2&refresh=false", "alice")
	if calls != 4 || refresh.Header().Get("X-Cache") != "REFRESH" || hit.Header().Get("X-Cache") != "HIT" || refresh.Body.String() != hit.Body.String() {
		t.Fatal(calls, refresh.Header(), hit.Header())
	}
	s.options.ResponseCacheScope = func(context.Context, core.Principal, string) (string, error) { return "updated-group", nil }
	if got := cacheRequest(s, "/api/account/usage?a=1&b=2", "alice"); got.Header().Get("X-Cache") != "MISS" {
		t.Fatal(got.Header())
	}
	s.options.ResponseCacheScope = func(context.Context, core.Principal, string) (string, error) {
		return "", core.NewError(core.Forbidden, "denied")
	}
	if got := cacheRequest(s, "/api/account/usage?a=1&b=2", "alice"); got.Code != 403 || got.Header().Get("X-Cache") != "" {
		t.Fatal(got.Code, got.Header())
	}
}
func TestResponseCacheErrorsControlsAndOptIn(t *testing.T) {
	s := cacheServer(t)
	var calls atomic.Int32
	s.handle("GET /api/account/quotas", func(r *http.Request) (any, error) {
		calls.Add(1)
		return nil, core.NewError(core.Unavailable, "fixture")
	})
	for range 2 {
		w := cacheRequest(s, "/api/account/quotas", "alice")
		if w.Code != 503 || w.Header().Get("X-Cache-Written-At") != "" {
			t.Fatal(w.Code, w.Header())
		}
	}
	if calls.Load() != 2 {
		t.Fatal("failure cached")
	}
	for _, q := range []string{"refresh=yes", "refresh=true&refresh=false", "x=%zz"} {
		w := cacheRequest(s, "/api/account/quotas?"+q, "alice")
		if w.Code != 400 {
			t.Fatal(q, w.Code)
		}
	}
	s.handle("GET /api/models", func(*http.Request) (any, error) { return "live", nil })
	if w := cacheRequest(s, "/api/models", "alice"); w.Header().Get("X-Cache") != "" {
		t.Fatal(w.Header())
	}
	s.options.ResponseCache = map[string]time.Duration{"GET /api/sandbox": 0}
	s.handle("GET /api/sandbox", func(*http.Request) (any, error) { return "disabled", nil })
	if w := cacheRequest(s, "/api/sandbox", "alice"); w.Header().Get("X-Cache") != "" {
		t.Fatal(w.Header())
	}
	s.handle("GET /api/admin/usage", func(*http.Request) (any, error) { return json.RawMessage("bad-json"), nil })
	if w := cacheRequest(s, "/api/admin/usage", "alice"); w.Code != 500 || w.Header().Get("X-Cache") != "" {
		t.Fatal(w.Code, w.Header())
	}
	s.options.ResponseCacheScope = nil
	s.handle("GET /api/sandboxes", func(*http.Request) (any, error) { return "no-scope-cache-bypass", nil })
	if w := cacheRequest(s, "/api/sandboxes", "alice"); w.Code != 200 || w.Header().Get("X-Cache") != "" {
		t.Fatal(w.Code, w.Header())
	}
}
func TestResponseCacheGETOnlyConfiguration(t *testing.T) {
	for pattern, ttl := range defaultResponseCache {
		if !strings.HasPrefix(pattern, "GET ") || ttl != 5*time.Minute {
			t.Fatal(pattern, ttl)
		}
	}
	defer func() {
		if recover() == nil {
			t.Error("POST cache accepted")
		}
	}()
	responseCacheTTL("POST /api/models", map[string]time.Duration{"POST /api/models": time.Minute})
}
func TestResponseCacheCancellationNotStored(t *testing.T) {
	s := cacheServer(t)
	s.handle("GET /api/admin/usage", func(r *http.Request) (any, error) { return nil, context.Canceled })
	w := cacheRequest(s, "/api/admin/usage", "alice")
	if w.Code == 200 || w.Header().Get("X-Cache") != "" {
		t.Fatal(w.Code, w.Header())
	}
}
