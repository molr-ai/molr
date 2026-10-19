package api

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/qq"
	"net/http"
)

func (s *HTTPServer) qqRoutes() {
	if s.options.QQ == nil {
		return
	}
	s.handle("GET /api/assistants/{id}/qq", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.options.QQ.Status(r.Context(), principal(r), id)
	})
	route(s, "POST /api/qq/connect", func(ctx context.Context, p core.Principal, r qq.ConnectRequest) (any, error) {
		return s.options.QQ.Connect(ctx, p, r)
	})
	route(s, "POST /api/qq/login", func(ctx context.Context, p core.Principal, r struct{ AssistantID core.ID }) (any, error) {
		return s.options.QQ.StartLogin(ctx, p, r.AssistantID)
	})
	route(s, "POST /api/qq/poll", func(ctx context.Context, p core.Principal, r struct{ AssistantID, ID core.ID }) (any, error) {
		return s.options.QQ.PollLogin(ctx, p, r.AssistantID, r.ID)
	})
	route(s, "POST /api/qq/cancel", func(ctx context.Context, p core.Principal, r struct{ AssistantID, ID core.ID }) (any, error) {
		return nil, s.options.QQ.CancelLogin(ctx, p, r.AssistantID, r.ID)
	})
	route(s, "POST /api/qq/disconnect", func(ctx context.Context, p core.Principal, r struct{ AssistantID core.ID }) (any, error) {
		return nil, s.options.QQ.Disconnect(ctx, p, r.AssistantID)
	})
}
