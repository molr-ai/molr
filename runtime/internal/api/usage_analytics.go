package api

import (
	"molr.ai/runtime/internal/core"
	"net/http"
	"strconv"
)

// UsageQuery selects calendar dates in Timezone; Until is exclusive.
type UsageQuery struct {
	From, Until, Timezone, GroupBy, Sort, Day, Search string
	AssistantID, ModelID, UserID, GroupID             core.ID
	Page                                              int
}

func (s *HTTPServer) usageAnalyticsRoutes() {
	for _, admin := range []bool{false, true} {
		path := "GET /api/account/usage"
		if admin {
			path = "GET /api/admin/usage/analytics"
		}
		s.handle(path, func(r *http.Request) (any, error) {
			if admin {
				if err := s.options.Accounts.RequireAdministrator(r.Context(), principal(r)); err != nil {
					return nil, err
				}
			}
			if s.options.UsageAnalytics == nil {
				return nil, core.NewError(core.Unavailable, "statistics unavailable")
			}
			v := r.URL.Query()
			page := 1
			if v.Get("page") != "" {
				var err error
				page, err = strconv.Atoi(v.Get("page"))
				if err != nil || page < 1 || page > 100000 {
					return nil, core.Invalid("page", "invalid page")
				}
			}
			q := UsageQuery{From: v.Get("from"), Until: v.Get("until"), Timezone: v.Get("timezone"), GroupBy: v.Get("group_by"), Sort: v.Get("sort"), Day: v.Get("day"), Search: v.Get("search"), AssistantID: core.ID(v.Get("assistant_id")), ModelID: core.ID(v.Get("model_id")), UserID: core.ID(v.Get("user_id")), GroupID: core.ID(v.Get("group_id")), Page: page}
			return s.options.UsageAnalytics(r.Context(), principal(r), q, admin)
		})
	}
}
