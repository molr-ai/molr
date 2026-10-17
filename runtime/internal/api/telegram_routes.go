package api

import (
	"context"
	"molr.ai/runtime/internal/core"
	"net/http"
)

func (s *HTTPServer) telegramRoutes() {
	if s.options.Telegram == nil {
		return
	}
	s.handle("GET /api/assistants/{id}/telegram", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.options.Telegram.Status(r.Context(), principal(r), id)
	})
	route(s, "POST /api/telegram/login", func(ctx context.Context, p core.Principal, r struct {
		AssistantID core.ID
		Token       string
	}) (any, error) {
		return s.options.Telegram.StartLogin(ctx, p, r.AssistantID, r.Token)
	})
	route(s, "POST /api/telegram/poll", func(ctx context.Context, p core.Principal, r struct{ AssistantID, ID core.ID }) (any, error) {
		return s.options.Telegram.PollLogin(ctx, p, r.AssistantID, r.ID)
	})
	route(s, "POST /api/telegram/confirm", func(ctx context.Context, p core.Principal, r struct{ AssistantID, ID core.ID }) (any, error) {
		return nil, s.options.Telegram.Confirm(ctx, p, r.AssistantID, r.ID)
	})
	route(s, "POST /api/telegram/cancel", func(ctx context.Context, p core.Principal, r struct{ AssistantID, ID core.ID }) (any, error) {
		return nil, s.options.Telegram.CancelLogin(ctx, p, r.AssistantID, r.ID)
	})
	route(s, "POST /api/telegram/disconnect", func(ctx context.Context, p core.Principal, r struct{ AssistantID core.ID }) (any, error) {
		return nil, s.options.Telegram.Disconnect(ctx, p, r.AssistantID)
	})
}
