package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/pricing"
)

// Check live identity, scope and group visibility before any cache lookup. Group
// changes get a different key, including administrator-to-member transitions.
func responseCacheScope(ctx context.Context, store persistence.Store, identity *auth.Manager, permissions policy.Service, preferences settings.Service, exchange *pricing.Exchange, p core.Principal, pattern string) (string, error) {
	if err := p.RequireOwner(); err != nil {
		return "", err
	}
	var fingerprint []byte
	err := store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		user, err := identity.CurrentUser(ctx, p)
		if err != nil {
			return err
		}
		group, err := auth.GroupFor(ctx, tx.Store(), p.OwnerID)
		if err != nil {
			return err
		}
		if strings.HasPrefix(pattern, "GET /api/admin/") {
			if err := identity.RequireAdministrator(ctx, p); err != nil {
				return err
			}
		}
		switch pattern {
		case "GET /api/sandbox", "GET /api/sandboxes", "GET /api/sandboxes/{id}/resources":
			action := policy.Read
			if pattern == "GET /api/sandboxes/{id}/resources" {
				action = policy.Manage
			}
			if err := permissions.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "resource"}, Action: action}); err != nil {
				return err
			}
			fingerprint, err = json.Marshal([]any{user.Version, group})
			return err
		case "GET /api/account/quotas", "GET /api/admin/usage":
			fingerprint, err = json.Marshal([]any{user.Version, group})
			return err
		case "GET /api/account/usage", "GET /api/admin/usage/analytics":
			prefs, err := preferences.Get(ctx, p)
			if err != nil {
				return err
			}
			fingerprint, err = json.Marshal([]any{user.Version, group, prefs.Currency, prefs.Timezone, exchange.Snapshot()})
			return err
		default:
			return core.Invalid("response_cache", "route requires an explicit authorization scope")
		}
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(fingerprint)
	return hex.EncodeToString(digest[:]), nil
}
