package api

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/weixin"
	"net/http"
)

func (s *HTTPServer) weixinRoutes() {
	if s.options.Weixin == nil {
		return
	}
	s.handle("GET /api/assistants/{id}/weixin", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.options.Weixin.Status(r.Context(), principal(r), id)
	})
	route(s, "POST /api/weixin/cancel", func(ctx context.Context, p core.Principal, r struct {
		AssistantID core.ID
		ID          core.ID
	}) (any, error) {
		return nil, s.options.Weixin.CancelLogin(ctx, p, r.AssistantID, r.ID)
	})
	route(s, "POST /api/weixin/login", func(ctx context.Context, p core.Principal, r struct{ AssistantID core.ID }) (any, error) {
		return s.options.Weixin.StartLogin(ctx, p, r.AssistantID)
	})
	route(s, "POST /api/weixin/poll", func(ctx context.Context, p core.Principal, r struct {
		AssistantID core.ID
		ID          core.ID
		VerifyCode  string
	}) (any, error) {
		return s.options.Weixin.PollLogin(ctx, p, r.AssistantID, weixin.PollRequest{ID: r.ID, VerifyCode: r.VerifyCode})
	})
	route(s, "POST /api/weixin/disconnect", func(ctx context.Context, p core.Principal, r struct{ AssistantID core.ID }) (any, error) {
		return nil, s.options.Weixin.Disconnect(ctx, p, r.AssistantID)
	})
}
