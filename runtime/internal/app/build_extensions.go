package app

import (
	"context"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/plan"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/plugins"
	"molr.ai/runtime/internal/policy"
	"net/http"
	"time"
)

func buildExtensions(adapters adapters, permissions *policy.Manager, identity *auth.Manager, vault *credential.Vault, plans *plan.Coordinator, wiring *buildWiring) {
	store := adapters.store
	access := access{store}
	wiring.plugins = plugins.New(store, plugins.Options{
		CheckPlugin: func(ctx context.Context, p core.Principal, id string) error {
			if err := identity.ValidatePrincipal(ctx, p); err != nil {
				return err
			}
			_, err := groupReference(ctx, store, p.OwnerID, "plugin", plugins.Key(id))
			return err
		},
		CheckTool: func(ctx context.Context, p core.Principal, name string) error {
			return permissions.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "tool", ID: core.ID(name)}, Action: policy.Execute})
		},
		Credentials: vault, Broker: vault,
		AuthorizeSettings: func(ctx context.Context, p core.Principal) error {
			return permissions.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "settings", ID: p.OwnerID}, Action: policy.Write})
		},
		CheckConnection: func(ctx context.Context, p core.Principal, id core.ID) error {
			if err := identity.ValidatePrincipal(ctx, p); err != nil {
				return err
			}
			_, err := groupReference(ctx, store, p.OwnerID, "connection", id)
			return err
		},
		CheckConnectionQuota: func(ctx context.Context, p core.Principal, id core.ID) error {
			return checkPluginConnectionQuota(ctx, store, p, id)
		},
		HTTPClient: func(ctx context.Context, p core.Principal, e core.Execution, id core.ID, origin string) (*http.Client, error) {
			binding := func(ctx context.Context, p core.Principal, id core.ID) (platform.NetworkBinding, error) {
				_, provider, err := wiring.plugins.ConnectionAccess(ctx, p, id)
				return platform.NetworkBinding{Origin: provider.Origin}, err
			}
			guard := platform.NewNetworkGuard(platform.NetworkOptions{AllowedPrivateAddresses: adapters.privateAddresses(), OwnerBinding: binding, Binding: func(ctx context.Context, e core.Execution, id core.ID) (platform.NetworkBinding, error) {
				if err := access.execution(ctx, e); err != nil {
					return platform.NetworkBinding{}, err
				}
				return binding(ctx, e.Principal, id)
			}})
			if p.Kind == core.ActorOwner {
				return guard.OwnerHTTPClient(p, id, origin), nil
			}
			return guard.HTTPClient(e, id, origin), nil
		},
		IsAdministrator: func(ctx context.Context, p core.Principal) (bool, error) {
			g, err := groupPolicy(ctx, store, p.OwnerID)
			return g.Administrator, err
		},
		UserBinding: func(ctx context.Context, p core.Principal, id core.ID, v plugins.Installation) (plugins.Binding, bool, error) {
			return groupPluginBinding(ctx, store, p, id, v)
		},
		Authorize: func(ctx context.Context, p core.Principal, write bool) error {
			if write {
				if err := identity.RequireAdministrator(ctx, p); err != nil {
					return err
				}
			}
			action := policy.Read
			if write {
				action = policy.Write
			}
			return permissions.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "settings", ID: p.OwnerID}, Action: action})
		},
		CheckAssistant: access.assistant,
		ReadPlans: func(ctx context.Context, p core.Principal) (any, error) {
			return plans.List(ctx, p, time.Time{}, time.Time{})
		},
	})
	wiring.mcp = newMCPService(store, identity, permissions, vault, adapters)
}
