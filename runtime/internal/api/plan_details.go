package api

import (
	"molr.ai/runtime/internal/business/schedule"
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
	if ref.Kind != "todo" && ref.Kind != "schedule" && ref.Kind != "watch" {
		return ref, core.Invalid("kind", "invalid plan kind")
	}
	return ref, nil
}
func (s *HTTPServer) planDetailRoutes() {
	s.handle("GET /api/plans/watches", func(r *http.Request) (any, error) {
		page, err := queryPage(r)
		if err != nil {
			return nil, err
		}
		return s.services.Plans.WatchPage(r.Context(), principal(r), r.URL.Query().Get("scope"), r.URL.Query().Get("state"), page)
	})
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
		boundOccurrence := false
		if id := queryID(r, "task_id"); id != "" {
			if err := id.Validate(); err != nil {
				return nil, err
			}
			task, err := s.services.Tasks.Get(r.Context(), p, id)
			if err != nil {
				return nil, err
			}
			ref, at = task.Plan, task.OccurrenceAt
			boundOccurrence = at != nil
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
		executionID := queryID(r, "execution_id")
		if executionID != "" {
			if taskID := queryID(r, "task_id"); taskID != "" && taskID != executionID {
				return nil, core.Invalid("execution_id", "conflicting execution selection")
			}
			if err := executionID.Validate(); err != nil {
				return nil, err
			}
			run, err := s.services.Plans.Execution(r.Context(), p, ref, executionID)
			if err != nil {
				return nil, err
			}
			at = run.OccurrenceAt
			boundOccurrence = at != nil
		}
		if raw := r.URL.Query().Get("at"); raw != "" {
			requested, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				return nil, core.Invalid("at", "invalid date")
			}
			if at != nil && !at.Equal(requested) {
				return nil, core.Invalid("at", "execution belongs to another occurrence")
			}
			at = &requested
		}
		plan, err := s.services.Plans.Detail(r.Context(), p, ref, at)
		if err != nil {
			return nil, err
		}
		if at != nil && !boundOccurrence {
			valid := false
			if plan.Schedule != nil {
				times, err := schedule.CalendarTimes(r.Context(), *plan.Schedule, *at, at.Add(time.Second))
				if err != nil {
					return nil, err
				}
				for _, value := range times {
					valid = valid || value.Equal(*at)
				}
			} else if plan.Todo != nil {
				for _, value := range []*time.Time{plan.Todo.StartAt, plan.Todo.DueAt} {
					valid = valid || value != nil && value.Equal(*at)
				}
			}
			if !valid {
				return nil, core.Invalid("at", "not a planned occurrence")
			}
		}
		return struct {
			Plan any
			At   *time.Time
		}{plan, at}, nil
	})
}
