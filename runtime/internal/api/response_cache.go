package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/resultcache"
)

// Only these read-only JSON routes opt in. A zero duration disables a route.
// Shared catalogs, credentials, commands, streams and execution admission do not.
var defaultResponseCache = map[string]time.Duration{
	"GET /api/account/usage":            5 * time.Minute,
	"GET /api/account/quotas":           5 * time.Minute,
	"GET /api/admin/usage/analytics":    5 * time.Minute,
	"GET /api/admin/usage":              5 * time.Minute,
	"GET /api/sandbox":                  5 * time.Minute,
	"GET /api/sandboxes":                5 * time.Minute,
	"GET /api/sandboxes/{id}/resources": 5 * time.Minute,
}

func (s *HTTPServer) cachedResponse(w http.ResponseWriter, r *http.Request, pattern string, ttl time.Duration, call func(*http.Request) (any, error)) {
	if s.options.ResponseCacheScope == nil {
		// Standalone API compositions must supply a live permission/scope check
		// before enabling response reuse; lack of that dependency never bypasses it.
		out, err := call(r)
		if err != nil {
			fail(w, err, info(r).ID)
			return
		}
		sendJSON(w, http.StatusOK, out)
		return
	}
	scope, err := s.options.ResponseCacheScope(r.Context(), principal(r), pattern)
	if err != nil {
		fail(w, err, info(r).ID)
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		fail(w, core.Invalid("query", "invalid query encoding"), info(r).ID)
		return
	}
	refresh := false
	if values, ok := query["refresh"]; ok {
		if len(values) != 1 || (values[0] != "true" && values[0] != "false") {
			fail(w, core.Invalid("refresh", "true or false required"), info(r).ID)
			return
		}
		refresh = values[0] == "true"
		query.Del("refresh")
	}
	p := principal(r)
	raw, _ := json.Marshal([]any{p.OwnerID, p.Kind, p.SubjectID, p.TokenID, pattern, r.URL.Path, query.Encode(), scope})
	digest := sha256.Sum256(raw)
	key := hex.EncodeToString(digest[:])
	result, err := s.responses.GetOrCompute(r.Context(), key, ttl, refresh, func(ctx context.Context) ([]byte, error) {
		request := r.Clone(ctx)
		request.URL.RawQuery = query.Encode()
		out, err := call(request)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(out)
		if err != nil {
			return nil, core.Wrap(core.Internal, "cannot encode cached response", err)
		}
		return append(encoded, '\n'), nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			err = core.Wrap(core.Canceled, "request canceled", err)
		} else if errors.Is(err, context.DeadlineExceeded) {
			err = core.Wrap(core.DeadlineExceeded, "request timed out", err)
		} else if errors.Is(err, resultcache.ErrBusy) || errors.Is(err, resultcache.ErrClosed) {
			err = core.Wrap(core.Unavailable, "response cache unavailable", err)
		}
		fail(w, err, info(r).ID)
		return
	}
	state := "MISS"
	if !result.Stored {
		state = "BYPASS"
	} else if result.Hit {
		state = "HIT"
	} else if refresh {
		state = "REFRESH"
	}
	w.Header().Set("X-Cache", state)
	if result.Stored {
		w.Header().Set("X-Cache-Written-At", result.WrittenAt.UTC().Format(time.RFC3339Nano))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.Value)
}

func responseCacheTTL(pattern string, overrides map[string]time.Duration) time.Duration {
	ttl := defaultResponseCache[pattern]
	if value, ok := overrides[pattern]; ok {
		ttl = value
	}
	if ttl > 0 && !strings.HasPrefix(pattern, "GET ") {
		panic("response caching requires a GET route")
	}
	return ttl
}
