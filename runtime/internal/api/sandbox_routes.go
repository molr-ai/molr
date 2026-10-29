package api

import (
	"encoding/json"
	"fmt"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"net/http"
	"strconv"
	"time"
)

func (s *HTTPServer) sandboxRoutes() {
	s.handle("POST /api/assistants/{id}/terminals", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		raw, err := decode[json.RawMessage](r)
		if err != nil {
			return nil, err
		}
		if s.options.TerminalStart == nil {
			return nil, core.NewError(core.Unavailable, "terminal unavailable")
		}
		return s.options.TerminalStart(r.Context(), principal(r), id, raw)
	})
	s.mux.HandleFunc("GET /api/assistants/{id}/terminals/{task}/stream", func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
		if err != nil {
			fail(w, err, info(r).ID)
			return
		}
		task := core.ID(r.PathValue("task"))
		cursor, err := strconv.ParseInt(r.URL.Query().Get("cursor"), 10, 64)
		if err != nil || cursor < 0 || task.Validate() != nil {
			fail(w, core.Invalid("cursor", "valid task and byte cursor required"), info(r).ID)
			return
		}
		if s.options.TerminalStream == nil {
			fail(w, core.NewError(core.Unavailable, "terminal unavailable"), info(r).ID)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		controller := http.NewResponseController(w)
		started := false
		err = s.options.TerminalStream(r.Context(), principal(r), id, task, cursor, func(raw json.RawMessage) error {
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

	invoke := func(r *http.Request, action string, raw json.RawMessage) (any, error) {
		if s.options.SandboxAdmin == nil {
			return nil, core.NewError(core.Unavailable, "sandbox manager unavailable")
		}
		return s.options.SandboxAdmin(r.Context(), principal(r), action, raw)
	}
	s.handle("GET /api/assistants/{id}/shell-tasks", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		if s.options.ShellTasks == nil {
			return nil, core.NewError(core.Unavailable, "shell tasks unavailable")
		}
		return s.options.ShellTasks(r.Context(), principal(r), id, tools.ShellTaskRequest{Action: "list"})
	})
	s.handle("POST /api/assistants/{id}/shell-tasks", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		raw, err := decode[json.RawMessage](r)
		if err != nil {
			return nil, err
		}
		if s.options.ShellTasks == nil {
			return nil, core.NewError(core.Unavailable, "shell tasks unavailable")
		}
		if err := principal(r).RequireOwner(); err != nil {
			return nil, err
		}
		q, err := tools.ParseShellTask(raw)
		if err != nil {
			return nil, err
		}
		return s.options.ShellTasks(r.Context(), principal(r), id, q)
	})
	s.handle("GET /api/resource-usage", func(r *http.Request) (any, error) {
		raw, _ := json.Marshal(map[string]any{"AssistantID": r.URL.Query().Get("assistant_id"), "ResourceID": r.URL.Query().Get("resource_id")})
		return invoke(r, "quota_usage", raw)
	})
	s.handle("GET /api/tools", func(r *http.Request) (any, error) { return invoke(r, "tools", nil) })
	s.handle("GET /api/sandboxes", func(r *http.Request) (any, error) { return invoke(r, "list", nil) })
	for _, action := range []string{"connect", "disconnect", "resource", "test", "rename"} {
		s.handle("POST /api/sandboxes/"+action, func(r *http.Request) (any, error) {
			raw, err := decode[json.RawMessage](r)
			if err != nil {
				return nil, err
			}
			return invoke(r, action, raw)
		})
	}
	s.handle("GET /api/sandboxes/{id}/resources", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(map[string]any{"ID": id})
		return invoke(r, "details", raw)
	})
}
