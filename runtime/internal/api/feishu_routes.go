package api

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/feishu"
	"net/http"
)

func (s *HTTPServer) feishuRoutes() {
	if s.options.Feishu == nil {
		return
	}
	s.handle("GET /api/assistants/{id}/feishu", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.options.Feishu.Status(r.Context(), principal(r), id)
	})
	route(s, "POST /api/feishu/connect", func(ctx context.Context, p core.Principal, r feishu.ConnectRequest) (any, error) {
		return s.options.Feishu.Connect(ctx, p, r)
	})
	route(s, "POST /api/feishu/login", func(ctx context.Context, p core.Principal, r struct{ AssistantID core.ID }) (any, error) {
		return s.options.Feishu.StartLogin(ctx, p, r.AssistantID)
	})
	route(s, "POST /api/feishu/poll", func(ctx context.Context, p core.Principal, r struct{ AssistantID, ID core.ID }) (any, error) {
		return s.options.Feishu.PollLogin(ctx, p, r.AssistantID, r.ID)
	})
	route(s, "POST /api/feishu/cancel", func(ctx context.Context, p core.Principal, r struct{ AssistantID, ID core.ID }) (any, error) {
		return nil, s.options.Feishu.CancelLogin(ctx, p, r.AssistantID, r.ID)
	})
	route(s, "POST /api/feishu/disconnect", func(ctx context.Context, p core.Principal, r struct{ AssistantID core.ID }) (any, error) {
		return nil, s.options.Feishu.Disconnect(ctx, p, r.AssistantID)
	})
}
