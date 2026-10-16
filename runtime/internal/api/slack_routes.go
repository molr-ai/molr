package api

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/slack"
	"net/http"
)

func (s *HTTPServer) slackRoutes() {
	if s.options.Slack == nil {
		return
	}
	s.handle("GET /api/assistants/{id}/slack", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.options.Slack.Status(r.Context(), principal(r), id)
	})
	route(s, "POST /api/slack/connect", func(ctx context.Context, p core.Principal, r slack.ConnectRequest) (any, error) {
		return s.options.Slack.StartLogin(ctx, p, r)
	})
	route(s, "POST /api/slack/disconnect", func(ctx context.Context, p core.Principal, r struct{ AssistantID core.ID }) (any, error) {
		return nil, s.options.Slack.Disconnect(ctx, p, r.AssistantID)
	})
}
