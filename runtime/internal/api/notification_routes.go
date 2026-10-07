package api

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/notification"
	"net/http"
)

func (s *HTTPServer) notificationRoutes() {
	if s.options.Notifications == nil {
		return
	}
	s.handle("GET /api/notifications", func(r *http.Request) (any, error) {
		q, err := queryPage(r)
		if err != nil {
			return nil, err
		}
		return s.options.Notifications.List(r.Context(), principal(r), notification.Query{Filter: r.URL.Query().Get("filter"), Kind: r.URL.Query().Get("kind"), AssistantID: core.ID(r.URL.Query().Get("assistant_id")), Page: q})
	})
	s.handle("GET /api/notifications/{id}", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.options.Notifications.Get(r.Context(), principal(r), id)
	})
	route(s, "POST /api/notifications/update", func(ctx context.Context, p core.Principal, r struct {
		IDs    []core.ID
		Action string
	}) (any, error) {
		return nil, s.options.Notifications.Update(ctx, p, r.IDs, r.Action)
	})
}
