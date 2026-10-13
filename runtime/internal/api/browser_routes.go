package api

import (
	"encoding/json"
	"fmt"
	"molr.ai/runtime/internal/core"
	"net/http"
	"strings"
	"time"
)

func (s *HTTPServer) browserRoutes() {
	for _, method := range []string{"GET", "PUT"} {
		s.handle(method+" /api/components/browser/settings", func(r *http.Request) (any, error) {
			if s.options.BrowserSettings == nil {
				return nil, core.NewError(core.Unavailable, "browser settings unavailable")
			}
			var raw json.RawMessage
			if r.Method == "PUT" {
				var err error
				raw, err = decode[json.RawMessage](r)
				if err != nil {
					return nil, err
				}
			}
			return s.options.BrowserSettings(r.Context(), principal(r), raw)
		})
	}

	s.handle("GET /api/assistants/{id}/browser", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		if s.options.Browser == nil {
			return nil, core.NewError(core.Unavailable, "browser unavailable")
		}
		return s.options.Browser(r.Context(), principal(r), id, json.RawMessage(`{"action":"status"}`))
	})
	s.handle("POST /api/assistants/{id}/browser", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		if s.options.Browser == nil {
			return nil, core.NewError(core.Unavailable, "browser unavailable")
		}
		raw, err := decode[json.RawMessage](r)
		if err != nil {
			return nil, err
		}
		result, err := s.options.Browser(r.Context(), principal(r), id, raw)
		return result, maintenanceError(err)
	})
	s.mux.HandleFunc("GET /api/assistants/{id}/browser/stream", func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
		if err != nil {
			fail(w, err, info(r).ID)
			return
		}
		task := core.ID(r.URL.Query().Get("task_id"))
		if task.Validate() != nil {
			fail(w, core.Invalid("task_id", "required"), info(r).ID)
			return
		}
		if s.options.BrowserStream == nil {
			fail(w, core.NewError(core.Unavailable, "browser unavailable"), info(r).ID)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		controller := http.NewResponseController(w)
		started := false
		err = s.options.BrowserStream(r.Context(), principal(r), id, task, func(raw json.RawMessage) error {
			if !json.Valid(raw) || strings.ContainsAny(string(raw), "\n\r") {
				return core.Invalid("frame", "invalid stream message")
			}
			controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
			started = true
			if _, err := fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
				return err
			}
			return controller.Flush()
		})
		if !started && err != nil {
			w.Header().Set("Content-Type", "application/json")
			fail(w, err, info(r).ID)
		}
	})
}
