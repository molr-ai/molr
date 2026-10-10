package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/debugtrace"
)

func (s *HTTPServer) debugRoutes() {
	s.handle("GET /api/debug/retention", func(r *http.Request) (any, error) { return s.services.Debug.Retention(r.Context(), principal(r)) })
	s.handle("GET /api/admin/debug/retention", func(r *http.Request) (any, error) { return s.services.Debug.AdminRetention(r.Context(), principal(r)) })
	s.handle("POST /api/admin/debug/retention", func(r *http.Request) (any, error) {
		q, err := decode[debugtrace.RetentionRequest](r)
		if err != nil {
			return nil, err
		}
		return s.services.Debug.SetRetention(r.Context(), principal(r), q)
	})
	s.handle("GET /api/debug/trajectory", func(r *http.Request) (any, error) {
		page, err := queryPage(r)
		if err != nil {
			return nil, err
		}
		return s.services.Debug.Trajectory(r.Context(), principal(r), debugtrace.TrajectoryQuery{Query: debugtrace.Query{AssistantID: queryID(r, "assistant_id"), Page: page}, After: r.URL.Query().Get("after")})
	})
	s.handle("GET /api/debug/runs", func(r *http.Request) (any, error) {
		page, err := queryPage(r)
		if err != nil {
			return nil, err
		}
		return s.services.Debug.Runs(r.Context(), principal(r), debugtrace.Query{AssistantID: queryID(r, "assistant_id"), Page: page})
	})
	s.handle("GET /api/debug/runs/{id}", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.services.Debug.Get(r.Context(), principal(r), id)
	})
	s.handle("GET /api/debug/runs/{id}/steps/{step}", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		step, err := strconv.Atoi(r.PathValue("step"))
		if err != nil {
			return nil, core.Invalid("step", "integer required")
		}
		return s.services.Debug.Detail(r.Context(), principal(r), id, step)
	})
	s.handle("GET /api/debug/assistants/{id}/checkpoint", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.services.Debug.Checkpoint(r.Context(), principal(r), id)
	})
	s.handle("POST /api/debug/assistants/{id}/context/reset", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		req, err := decode[debugtrace.ResetRequest](r)
		if err != nil {
			return nil, err
		}
		return nil, s.services.Debug.ResetContext(r.Context(), principal(r), id, req)
	})
	s.mux.HandleFunc("GET /api/debug/stream", s.debugStream)
}
func (s *HTTPServer) debugStream(w http.ResponseWriter, r *http.Request) {
	last := r.Header.Get("Last-Event-ID")
	if last == "" {
		last = r.URL.Query().Get("after")
	}
	var cursor uint64
	var err error
	if last != "" {
		cursor, err = strconv.ParseUint(last, 10, 64)
		if err != nil {
			fail(w, core.Invalid("after", "integer cursor required"), info(r).ID)
			return
		}
	}
	// Register both watches before validating and reading the current revision.
	// Notifications arriving during either read remain pending for the loop.
	changes, err := s.services.Debug.WatchRevision(r.Context(), principal(r))
	if err != nil {
		fail(w, err, info(r).ID)
		return
	}
	defer changes.Close()
	identity, err := s.services.Auth.WatchPrincipal(principal(r))
	if err != nil {
		fail(w, err, info(r).ID)
		return
	}
	defer identity.Close()
	remaining, err := identity.Check(r.Context())
	if err != nil {
		fail(w, err, info(r).ID)
		return
	}
	expiry := time.NewTimer(remaining)
	defer expiry.Stop()
	revision, err := s.services.Debug.Revision(r.Context(), principal(r))
	if err != nil {
		fail(w, err, info(r).ID)
		return
	}
	s.mu.Lock()
	if s.closing || s.streams >= s.config.HTTP.MaxSSEConnections {
		s.mu.Unlock()
		fail(w, core.NewError(core.RateLimited, "stream limit"), info(r).ID)
		return
	}
	s.streams++
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.streams--; s.mu.Unlock() }()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	control := http.NewResponseController(w)
	write := func(text string) bool {
		_ = control.SetWriteDeadline(time.Now().Add(s.config.HTTP.StreamWriteTimeout.Value()))
		if _, err := fmt.Fprint(w, text); err != nil {
			return false
		}
		return control.Flush() == nil
	}
	if !write(": connected\n\n") {
		return
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	authentication, _ := s.authentication(r)
	for {
		if revision != cursor {
			if s.checkStreamPrincipal(r.Context(), principal(r), authentication) != nil {
				return
			}
			if !write(fmt.Sprintf("id: %d\nevent: change\ndata: {\"Revision\":%d}\n\n", revision, revision)) {
				return
			}
			cursor = revision
		}
		select {
		case <-r.Context().Done():
			return
		case <-s.streamContext.Done():
			return
		case <-changes.Done():
			return
		case <-identity.Done():
			return
		case <-ticker.C:
			if !write(": heartbeat\n\n") {
				return
			}
			continue
		case <-changes.Changes():
		case <-identity.Changes():
		case <-expiry.C:
		}
		remaining, err = identity.Check(r.Context())
		if err != nil {
			return
		}
		expiry.Reset(remaining)
		revision, err = s.services.Debug.Revision(r.Context(), principal(r))
		if err != nil {
			write("event: disabled\ndata: {}\n\n")
			return
		}
	}
}
