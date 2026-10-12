package api

import (
	"errors"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/installation"
	"net/http"
)

func maintenanceError(err error) error {
	var e *installation.ComponentError
	if errors.As(err, &e) {
		code := core.FailedPrecondition
		if e.Code == "maintenance_busy" {
			code = core.Conflict
		}
		if e.Code == "maintenance_invalid_request" {
			code = core.InvalidArgument
		}
		return core.WithReason(core.NewError(code, e.Detail), core.Reason(e.Code))
	}
	return err
}
func (s *HTTPServer) maintenanceRoutes() {
	admin := func(r *http.Request) error {
		if s.options.Accounts == nil || s.options.Maintenance == nil {
			return core.NewError(core.Unavailable, "maintenance unavailable")
		}
		return s.options.Accounts.RequireAdministrator(r.Context(), principal(r))
	}
	s.handle("GET /api/components", func(r *http.Request) (any, error) {
		if err := admin(r); err != nil {
			return nil, err
		}
		return s.options.Maintenance.Snapshot(r.Context()), nil
	})
	s.handle("GET /api/maintenance", func(r *http.Request) (any, error) {
		if err := admin(r); err != nil {
			return nil, err
		}
		return map[string]any{"can_update": s.options.Maintenance.CanUpdate(r.Context()), "task": s.options.Maintenance.Latest(r.Context())}, nil
	})
	s.handle("GET /api/maintenance/tasks/{id}", func(r *http.Request) (any, error) {
		if err := admin(r); err != nil {
			return nil, err
		}
		v, err := s.options.Maintenance.Task(r.Context(), r.PathValue("id"))
		return v, maintenanceError(err)
	})
	s.handle("POST /api/maintenance/tasks/{id}/cancel", func(r *http.Request) (any, error) {
		if err := admin(r); err != nil {
			return nil, err
		}
		v, err := s.options.Maintenance.Cancel(r.Context(), r.PathValue("id"))
		return v, maintenanceError(err)
	})
	for _, operation := range []string{"install", "update", "repair"} {
		s.mux.HandleFunc("POST /api/components/{id}/"+operation, func(w http.ResponseWriter, r *http.Request) {
			if err := admin(r); err != nil {
				fail(w, err, info(r).ID)
				return
			}
			q, err := decode[struct {
				Key string `json:"idempotency_key"`
			}](r)
			if err != nil {
				fail(w, err, info(r).ID)
				return
			}
			v, err := s.options.Maintenance.Start(r.Context(), operation, r.PathValue("id"), q.Key, "")
			if err != nil {
				fail(w, maintenanceError(err), info(r).ID)
				return
			}
			sendJSON(w, http.StatusAccepted, v)
		})
	}
	s.mux.HandleFunc("POST /api/updates/apply", func(w http.ResponseWriter, r *http.Request) {
		if err := admin(r); err != nil {
			fail(w, err, info(r).ID)
			return
		}
		q, err := decode[struct {
			Key     string `json:"idempotency_key"`
			Version string `json:"version"`
		}](r)
		if err != nil {
			fail(w, err, info(r).ID)
			return
		}
		if s.options.Updates == nil {
			fail(w, core.NewError(core.Unavailable, "updates unavailable"), info(r).ID)
			return
		}
		snapshot := s.options.Updates.Snapshot()
		if snapshot.Latest == nil || snapshot.Latest.Version != q.Version {
			fail(w, core.Invalid("version", "check the configured update source first"), info(r).ID)
			return
		}
		v, err := s.options.Maintenance.Start(r.Context(), "update", "", q.Key, q.Version)
		if err != nil {
			fail(w, maintenanceError(err), info(r).ID)
			return
		}
		sendJSON(w, http.StatusAccepted, v)
	})
}
