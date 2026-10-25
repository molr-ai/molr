package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/api"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/buildinfo"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/maintenance"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/oauthbroker"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/updates"
	"molr.ai/runtime/internal/webassets"
	"net/netip"
	"net/url"
)

func (b *Bundle) buildHTTP(cfg config.Config, adapters adapters, identity *auth.Manager, permissions policy.Service, chatgptOAuth *chatgptProvider, maintenance maintenance.Service, toolset toolServices, wiring *buildWiring, requestLogger *slog.Logger) (*updates.Service, error) {
	store := b.store
	preferences := b.Services.Settings
	sandboxRouter, executor, permissionRequests := b.remote, toolset.executor, toolset.requests
	limiter, err := api.NewRateLimiter(store, api.RateLimitOptions{Limit: cfg.HTTP.RequestsPerMinute, CategoryLimits: map[string]int{"login": cfg.Auth.LoginAttemptsPerMinute}})
	if err != nil {
		return nil, err
	}
	build := buildinfo.Current()
	build.EmbeddedWebUI = webassets.Embedded
	installation := updates.ReadInstallation(cfg.ConfigPath, cfg.Updates.Source)
	updateSource := cfg.Updates.Source
	if installation != nil {
		updateSource = installation.Source
	}
	if build.Distribution == "container" {
		updateSource = "https://ghcr.io"
	}
	updateAddresses := adapters.privateAddresses()
	if u, err := url.Parse(updateSource); err == nil {
		if ip, err := netip.ParseAddr(u.Hostname()); err == nil && (ip.IsPrivate() || ip.IsLoopback()) {
			updateAddresses = append(updateAddresses, ip)
		}
	}
	updateService := updates.New(updateSource, cfg.Updates.AutoCheck, build, platform.ConfiguredHTTPClient(updateSource, updateAddresses))
	b.HTTP, err = api.NewServer(cfg, b.Services, api.Options{Pricing: b.priceUpdater, ResponseCacheScope: func(ctx context.Context, p core.Principal, pattern string) (string, error) {
		return responseCacheScope(ctx, store, identity, permissions, preferences, b.Exchange, p, pattern)
	}, OAuth: oauthbroker.New(map[string]oauthbroker.Provider{"chatgpt": chatgptOAuth}, identity.ValidatePrincipal), ChatGPTDisconnect: chatgptOAuth.disconnect, RequestLogger: requestLogger, Workspace: newWorkspaceExplorer(adapters, cfg.Execution.MaxUploadBytes), Skills: func(ctx context.Context, p core.Principal, id core.ID, path string) (any, error) {
		return inspectSkills(ctx, adapters, p, id, path)
	}, Maintenance: maintenance, Browser: func(ctx context.Context, p core.Principal, id core.ID, raw json.RawMessage) (json.RawMessage, error) {
		q, err := tools.ParseBrowser(raw)
		if err != nil {
			return nil, err
		}
		if q.Action.Action == "install_start" {
			if err := identity.RequireAdministrator(ctx, p); err != nil {
				return nil, err
			}
		}
		return b.shellTasks.BrowserManage(ctx, p, id, q)
	}, BrowserSettings: b.shellTasks.BrowserSettings, BrowserStream: b.shellTasks.BrowserFrames, Updates: updateService, UpgradeInfo: installation, Notifications: notification.New(store, permissions), ChatChannel: b.channelStatus, Weixin: b.weixin, Telegram: b.telegram, Slack: b.slack, Dingtalk: b.dingtalk, Feishu: b.feishu, QQ: b.qq, Accounts: identity, Setup: b.setup, DiscoverProviders: func(ctx context.Context, p core.Principal) (any, error) {
		if err := identity.RequireAdministrator(ctx, p); err != nil {
			return nil, err
		}
		return adapters.discoverLocalProviders(ctx, p)
	},
		AdminCatalog: func(ctx context.Context, p core.Principal) (any, error) {
			return groupCatalog(ctx, p, adapters, sandboxRouter, executor)
		},
		UsageAnalytics: func(ctx context.Context, p core.Principal, q api.UsageQuery, admin bool) (any, error) {
			prefs, err := preferences.Get(ctx, p)
			if err != nil {
				return nil, err
			}
			if q.Timezone == "" {
				q.Timezone = prefs.Timezone
			}
			return usageAnalytics(ctx, store, identity, p, q, admin, prefs.Currency, b.Exchange.Snapshot())
		},
		AdminUsage: func(ctx context.Context, p core.Principal) (any, error) { return adminUsage(ctx, store, identity, p) },
		AccountQuotas: func(ctx context.Context, p core.Principal) (any, error) {
			return accountQuotas(ctx, store, identity, p)
		}, Plugins: wiring.plugins, MCP: wiring.mcp, TerminalStart: b.shellTasks.StartTerminal, TerminalStream: b.shellTasks.TerminalFrames, ShellTasks: func(ctx context.Context, p core.Principal, id core.ID, raw json.RawMessage) (json.RawMessage, error) {
			if err := p.RequireOwner(); err != nil {
				return nil, err
			}
			q, err := tools.ParseShellTask(raw)
			if err != nil {
				return nil, err
			}
			return b.shellTasks.Manage(ctx, p, id, q)
		}, PermissionRequests: permissionRequests, SandboxAdmin: sandboxRouter.admin, SandboxInfo: func(ctx context.Context, p core.Principal) (any, error) {
			if err := p.RequireOwner(); err != nil {
				return nil, err
			}
			if err := permissions.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "resource"}, Action: policy.Read}); err != nil {
				return nil, err
			}
			caps := platform.Capabilities{Reason: "sandbox_disabled"}
			networks := []config.SandboxNetwork{}
			directories := []config.SandboxDirectory{}
			if b.sandbox != nil {
				caps, _ = b.sandbox.runner.Capabilities(ctx)
				if g, err := groupPolicy(ctx, store, p.OwnerID); err == nil && g.Administrator {
					networks = append(networks, cfg.Sandbox.Networks...)
					directories = append(directories, cfg.Sandbox.Directories...)
				}
			}
			return map[string]any{"Enabled": cfg.Sandbox.Enabled, "Storage": sandboxRouter.localInfo(ctx).Storage, "Capabilities": caps, "Networks": networks, "Directories": directories}, nil
		}, Limiter: limiter, PreLoginCSRF: identity.PreLoginCSRF})
	if err != nil {
		return nil, err
	}
	return updateService, nil
}
