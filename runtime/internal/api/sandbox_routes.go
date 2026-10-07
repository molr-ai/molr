package api

import (
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"net/http"
)

func (s *HTTPServer) sandboxRoutes() {
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
		return s.options.ShellTasks(r.Context(), principal(r), id, json.RawMessage(`{"action":"list"}`))
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
		return s.options.ShellTasks(r.Context(), principal(r), id, raw)
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
