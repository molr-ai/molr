package api

import (
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/mcpclient"
	"net/http"
)

func (s *HTTPServer) mcpRoutes() {
	if s.options.MCP == nil {
		return
	}
	s.handle("GET /api/mcp/connections", func(r *http.Request) (any, error) { return s.options.MCP.List(r.Context(), principal(r)) })
	s.handle("POST /api/mcp/connections/save", func(r *http.Request) (any, error) {
		q, err := decode[struct {
			ID                                                 core.ID
			Name, Endpoint, Auth, Header, APIKey, Verification string
			Enabled                                            bool
			EnabledTools                                       []core.ID
			CallLimit                                          int64
			Concurrent, TimeoutSeconds                         int
			Mutation                                           core.Mutation
		}](r)
		if err != nil {
			return nil, err
		}
		return s.options.MCP.Save(r.Context(), principal(r), mcpclient.SaveRequest{ID: q.ID, Name: q.Name, Endpoint: q.Endpoint, Auth: q.Auth, Header: q.Header, Enabled: q.Enabled, EnabledTools: q.EnabledTools, CallLimit: q.CallLimit, Concurrent: q.Concurrent, TimeoutSeconds: q.TimeoutSeconds, Mutation: q.Mutation, APIKey: core.NewSecret(q.APIKey), Verification: auth.Verification{Token: core.NewSecret(q.Verification)}})
	})
	s.handle("POST /api/mcp/connections/refresh", func(r *http.Request) (any, error) {
		q, err := decode[mcpclient.Request](r)
		if err != nil {
			return nil, err
		}
		return s.options.MCP.Refresh(r.Context(), principal(r), q)
	})
	s.handle("POST /api/mcp/connections/remove", func(r *http.Request) (any, error) {
		q, err := decode[struct {
			ID           core.ID
			Mutation     core.Mutation
			Verification string
		}](r)
		if err != nil {
			return nil, err
		}
		return nil, s.options.MCP.Remove(r.Context(), principal(r), mcpclient.Request{ID: q.ID, Mutation: q.Mutation, Verification: auth.Verification{Token: core.NewSecret(q.Verification)}})
	})
	s.handle("GET /api/mcp/connections/{id}/history", func(r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		return s.options.MCP.History(r.Context(), principal(r), id)
	})
}
