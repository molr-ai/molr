package api

import (
	"molr.ai/runtime/internal/core"
	"net/http"
	"time"
)

func planRef(r *http.Request) (core.Object, error) {
	id, err := pathID(r)
	ref := core.Object{Kind: r.PathValue("kind"), ID: id}
	if err != nil {
		return ref, err
	}
	if ref.Kind != "todo" && ref.Kind != "schedule" {
		return ref, core.Invalid("kind", "invalid plan kind")
	}
	return ref, nil
}
func (s *HTTPServer) planDetailRoutes() {
	s.handle("GET /api/plans/{kind}/{id}", func(r *http.Request) (any, error) {
		ref, err := planRef(r)
		if err != nil {
			return nil, err
		}
		var at *time.Time
		if raw := r.URL.Query().Get("at"); raw != "" {
			v, e := time.Parse(time.RFC3339Nano, raw)
			if e != nil {
				return nil, core.Invalid("at", "invalid date")
			}
			at = &v
		}
		return s.services.Plans.Detail(r.Context(), principal(r), ref, at)
	})
	s.handle("GET /api/plans/{kind}/{id}/history", func(r *http.Request) (any, error) {
		ref, err := planRef(r)
		if err != nil {
			return nil, err
		}
		page, err := queryPage(r)
		if err != nil {
			return nil, err
		}
		return s.services.Plans.History(r.Context(), principal(r), ref, page)
	})
	s.handle("GET /api/plans/{kind}/{id}/executions/{execution}", func(r *http.Request) (any, error) {
		ref, err := planRef(r)
		if err != nil {
			return nil, err
		}
		id := core.ID(r.PathValue("execution"))
		if err = id.Validate(); err != nil {
			return nil, err
		}
		return s.services.Plans.Execution(r.Context(), principal(r), ref, id)
	})
	// Deep links do not depend on the selected event being on the loaded page.
	s.handle("GET /api/plans/selection", func(r *http.Request) (any, error) {
		p := principal(r)
		var ref core.Object
		var at *time.Time
		if id := queryID(r, "task_id"); id != "" {
			if err := id.Validate(); err != nil {
				return nil, err
			}
			task, err := s.services.Tasks.Get(r.Context(), p, id)
			if err != nil {
				return nil, err
			}
			ref, at = task.Plan, task.OccurrenceAt
			if id := queryID(r, "plan_id"); id != "" && id != ref.ID {
				return nil, core.Invalid("plan_id", "task belongs to another plan")
			}
		} else {
			id := queryID(r, "plan_id")
			if err := id.Validate(); err != nil {
				return nil, err
			}
			var err error
			ref, err = s.services.Plans.Resolve(r.Context(), p, id)
			if err != nil {
				return nil, err
			}
		}
		plan, err := s.services.Plans.Detail(r.Context(), p, ref, at)
		if err != nil {
			return nil, err
		}
		return struct {
			Plan any
			At   *time.Time
		}{plan, at}, nil
	})
}
