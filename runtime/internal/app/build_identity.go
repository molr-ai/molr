package app

import (
	"context"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"strings"
)

func buildIdentity(store persistence.Store, cfg config.Config, key []byte, wiring *buildWiring) (*auth.Manager, error) {
	access := access{store}
	return auth.NewService(store, auth.Options{AfterAccessChange: func(ctx context.Context) {
		if wiring.plugins != nil {
			wiring.plugins.RevalidateRunning(ctx)
		}
		if wiring.mcp != nil {
			wiring.mcp.RevalidateRunning(ctx)
		}
	}, ValidateResources: func(ctx context.Context, p core.Principal, r []auth.ResourceReference) error {
		if wiring.validateReferences == nil {
			return core.NewError(core.Unavailable, "resource catalog unavailable")
		}
		return wiring.validateReferences(ctx, p, r)
	}, MasterKey: key, LocalHTTP: cfg.HTTP.TLSMode == config.LocalHTTP, LANHTTP: cfg.HTTP.TLSMode == config.LANHTTP, Origin: strings.TrimSuffix(cfg.HTTP.PublicURL, "/"), AllowedOrigins: cfg.HTTP.AllowedOrigins, SessionLifetime: cfg.Auth.SessionLifetime.Value(), SessionIdle: cfg.Auth.SessionIdle.Value(), VerificationLifetime: cfg.Auth.VerificationLifetime.Value(), LoginAttemptsPerMinute: cfg.Auth.LoginAttemptsPerMinute, ValidateAssistant: access.liveAssistant})
}
