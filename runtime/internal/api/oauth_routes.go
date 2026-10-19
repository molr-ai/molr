package api

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/oauthbroker"
	"net/http"
)

func (s *HTTPServer) oauthRoutes() {
	// Registered providers are an internal composition choice, never arbitrary
	// authorization/token endpoints supplied by a webpage or model.
	b := s.options.OAuth
	if b == nil {
		b = oauthbroker.New(nil, nil)
	}
	route(s, "POST /api/oauth/start", func(ctx context.Context, p core.Principal, q oauthbroker.StartRequest) (any, error) {
		return b.Start(ctx, p, q)
	})
	route(s, "POST /api/oauth/complete", func(ctx context.Context, p core.Principal, q oauthbroker.CompleteRequest) (any, error) {
		return b.Complete(ctx, p, q)
	})
	route(s, "POST /api/oauth/cancel", func(ctx context.Context, p core.Principal, q struct{ ID string }) (any, error) {
		return b.Cancel(ctx, p, q.ID)
	})
	s.handle("GET /api/oauth/attempts/{id}", func(r *http.Request) (any, error) { return b.Status(r.Context(), principal(r), r.PathValue("id")) })
}
