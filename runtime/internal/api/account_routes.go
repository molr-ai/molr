package api

import (
	"context"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"net/http"
)

func (s *HTTPServer) accountRoutes() {
	m := s.options.Accounts
	if m == nil {
		return
	}
	s.usageAnalyticsRoutes()
	s.handle("GET /api/account", func(r *http.Request) (any, error) { return m.CurrentUser(r.Context(), principal(r)) })
	route(s, "POST /api/account/profile", func(ctx context.Context, p core.Principal, q struct {
		DisplayName string
		Avatar      *string
		Mutation    core.Mutation
	}) (any, error) {
		return m.UpdateProfile(ctx, p, q.DisplayName, q.Avatar, q.Mutation.ExpectedVersion)
	})
	s.handle("GET /api/account/quotas", func(r *http.Request) (any, error) {
		if s.options.AccountQuotas == nil {
			return nil, core.NewError(core.Unavailable, "quota service unavailable")
		}
		return s.options.AccountQuotas(r.Context(), principal(r))
	})
	s.handle("GET /api/admin/users", func(r *http.Request) (any, error) {
		q, err := queryPage(r)
		if err != nil {
			return nil, err
		}
		return m.Users(r.Context(), principal(r), q)
	})
	route(s, "POST /api/admin/users", func(ctx context.Context, p core.Principal, q struct {
		Username, DisplayName, Password string
		GroupID                         core.ID
		Mutation                        core.Mutation
	}) (any, error) {
		return m.CreateUser(ctx, p, auth.CreateUserRequest{Username: q.Username, DisplayName: q.DisplayName, Password: core.NewSecret(q.Password), GroupID: q.GroupID, Mutation: q.Mutation})
	})
	route(s, "POST /api/admin/users/update", func(ctx context.Context, p core.Principal, q auth.UpdateUserRequest) (any, error) {
		return m.UpdateUser(ctx, p, q)
	})
	s.handle("GET /api/admin/groups", func(r *http.Request) (any, error) { return m.Groups(r.Context(), principal(r)) })
	route(s, "POST /api/admin/groups", func(ctx context.Context, p core.Principal, q auth.SaveGroupRequest) (any, error) {
		return m.SaveGroup(ctx, p, q)
	})
	route(s, "POST /api/admin/groups/delete", func(ctx context.Context, p core.Principal, q struct {
		ID       core.ID
		Mutation core.Mutation
	}) (any, error) {
		return nil, m.DeleteGroup(ctx, p, q.ID, q.Mutation.ExpectedVersion)
	})
	s.handle("GET /api/admin/catalog", func(r *http.Request) (any, error) {
		if err := m.RequireAdministrator(r.Context(), principal(r)); err != nil {
			return nil, err
		}
		if s.options.AdminCatalog == nil {
			return nil, core.NewError(core.Unavailable, "catalog unavailable")
		}
		return s.options.AdminCatalog(r.Context(), principal(r))
	})
	s.handle("GET /api/admin/usage", func(r *http.Request) (any, error) {
		if err := m.RequireAdministrator(r.Context(), principal(r)); err != nil {
			return nil, err
		}
		if s.options.AdminUsage == nil {
			return nil, core.NewError(core.Unavailable, "statistics unavailable")
		}
		return s.options.AdminUsage(r.Context(), principal(r))
	})
}
