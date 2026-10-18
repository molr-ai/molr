package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/mcpclient"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/plugins"
	"molr.ai/runtime/internal/policy"
)

type vaultManager interface {
	credential.Manager
	credential.Broker
}

func newMCPService(store persistence.Store, identity *auth.Manager, permissions policy.Service, vault vaultManager, a adapters) *mcpclient.Service {
	var service *mcpclient.Service
	service = mcpclient.New(store, mcpclient.Options{
		Authorize: func(ctx context.Context, p core.Principal, write bool) error {
			if write {
				return identity.RequireAdministrator(ctx, p)
			}
			return permissions.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "tool", ID: "catalog"}, Action: policy.Read})
		},
		IsAdministrator: func(ctx context.Context, p core.Principal) (bool, error) {
			g, err := groupPolicy(ctx, store, p.OwnerID)
			return g.Administrator, err
		},
		CheckConnection: func(ctx context.Context, p core.Principal, id core.ID) error {
			if err := identity.ValidatePrincipal(ctx, p); err != nil {
				return err
			}
			_, err := groupReference(ctx, store, p.OwnerID, "connection", id)
			return err
		},
		CheckTool: func(ctx context.Context, p core.Principal, id core.ID) error {
			if err := permissions.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "tool", ID: id}, Action: policy.Execute}); err != nil {
				return err
			}
			if p.Kind == core.ActorAssistant {
				return permissions.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: id}, ResourceID: id, Action: policy.Execute})
			}
			return nil
		},
		CheckConnectionQuota: func(ctx context.Context, p core.Principal, id core.ID) error {
			return checkPluginConnectionQuota(ctx, store, p, id)
		},
		Credentials: vault, Broker: vault,
		HTTPClient: func(ctx context.Context, p core.Principal, e core.Execution, id core.ID, origin string) (*http.Client, error) {
			binding := func(ctx context.Context, p core.Principal, id core.ID) (platform.NetworkBinding, error) {
				c, err := service.ConnectionAccess(ctx, p, id)
				return platform.NetworkBinding{Origin: c.Origin()}, err
			}
			guard := platform.NewNetworkGuard(platform.NetworkOptions{AllowedPrivateAddresses: a.privateAddresses(), OwnerBinding: binding, Binding: func(ctx context.Context, e core.Execution, id core.ID) (platform.NetworkBinding, error) {
				if err := (access{store}).execution(ctx, e); err != nil {
					return platform.NetworkBinding{}, err
				}
				return binding(ctx, e.Principal, id)
			}})
			if p.Kind == core.ActorOwner {
				return guard.OwnerHTTPClient(p, id, origin), nil
			}
			return guard.HTTPClient(e, id, origin), nil
		},
	})
	return service
}

type externalTools struct {
	plugin *plugins.Service
	mcp    *mcpclient.Service
}

func (e externalTools) Definitions(ctx context.Context, p core.Principal, catalog bool) ([]tools.Definition, error) {
	a, err := e.plugin.Definitions(ctx, p, catalog)
	if err != nil {
		return nil, err
	}
	b, err := e.mcp.Definitions(ctx, p, catalog)
	return append(a, b...), err
}
func (e externalTools) ValidateCall(ctx context.Context, c tools.Call) error {
	if strings.HasPrefix(c.Name, "mcp_") {
		return e.mcp.ValidateCall(ctx, c)
	}
	return e.plugin.ValidateCall(ctx, c)
}
func (e externalTools) PrepareCall(ctx context.Context, c tools.Call) (core.ID, error) {
	if strings.HasPrefix(c.Name, "mcp_") {
		return e.mcp.PrepareCall(ctx, c)
	}
	return e.plugin.PrepareCall(ctx, c)
}
func (e externalTools) Execute(ctx context.Context, c tools.Call) (json.RawMessage, error) {
	if strings.HasPrefix(c.Name, "mcp_") {
		return e.mcp.Execute(ctx, c)
	}
	return e.plugin.Execute(ctx, c)
}

type mcpGrants struct {
	policy.Service
	mcp *mcpclient.Service
}

func (g mcpGrants) Revoke(ctx context.Context, p core.Principal, q policy.RevokeRequest) error {
	err := g.Service.Revoke(ctx, p, q)
	if err == nil {
		g.mcp.RevalidateRunning(ctx)
	}
	return err
}
func (g mcpGrants) UpdateGrant(ctx context.Context, p core.Principal, q policy.UpdateGrantRequest) (policy.Grant, error) {
	out, err := g.Service.UpdateGrant(ctx, p, q)
	if err == nil {
		g.mcp.RevalidateRunning(ctx)
	}
	return out, err
}
