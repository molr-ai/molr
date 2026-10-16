package api

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/dingtalk"
	"net/http"
)

func (s *HTTPServer) dingtalkRoutes() {
	if s.options.Dingtalk == nil {
		return
	}
	s.handle("GET /api/assistants/{id}/dingtalk", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.options.Dingtalk.Status(r.Context(), principal(r), id)
	})
	route(s, "POST /api/dingtalk/connect", func(ctx context.Context, p core.Principal, r dingtalk.ConnectRequest) (any, error) {
		return s.options.Dingtalk.Start(ctx, p, r)
	})
	route(s, "POST /api/dingtalk/disconnect", func(ctx context.Context, p core.Principal, r struct{ AssistantID core.ID }) (any, error) {
		return nil, s.options.Dingtalk.Disconnect(ctx, p, r.AssistantID)
	})
}
