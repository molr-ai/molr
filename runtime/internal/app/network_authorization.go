package app

import (
	"context"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"slices"
	"time"
)

func (s *shellService) networkAuthorization(ctx context.Context, e core.Execution, n platform.NetworkAuthorization) (platform.NetworkAuthorization, error) {
	g, err := persistence.Load[policy.Grant](ctx, s.store, "security.grants", persistence.Key(e.Principal.OwnerID, n.GrantID))
	if err != nil {
		return n, err
	}
	if g.OwnerID != e.Principal.OwnerID || g.AssistantID != e.AssistantID || g.RevokedAt != nil || (g.ExpiresAt != nil && !g.ExpiresAt.After(time.Now())) || !slices.Contains(g.Actions, policy.Execute) {
		return n, denied()
	}
	repo := resource.NewRepository(s.store)
	r, err := repo.Get(ctx, g.OwnerID, g.ResourceID)
	if err != nil {
		return n, err
	}
	if r.Kind != resource.Network || r.State != resource.Available || sandboxOf(r) != "local" {
		return n, denied()
	}
	b, err := repo.Binding(ctx, g.OwnerID, g.ResourceID)
	if err != nil {
		return n, err
	}
	if b.SandboxTarget != n.Source {
		return n, denied()
	}
	n.ExpiresAt = g.ExpiresAt
	n.Constraints = nil
	if c := g.EffectiveConstraints(time.Now()); c != nil {
		n.Constraints = c.Network
	}
	if n.Constraints == nil {
		return n, denied()
	}
	return groupNetworkAuthorization(ctx, s.store, e.Principal.OwnerID, "local", n)
}

func (b *remoteSandboxBackend) networkAuthorization(ctx context.Context, e core.Execution, n platform.NetworkAuthorization) (platform.NetworkAuthorization, error) {
	value, ok := b.networkQuotas.Load(shellKey(e))
	if !ok {
		return n, denied()
	}
	for _, current := range value.([]platform.NetworkAuthorization) {
		if current.Source == n.Source && current.GrantID == n.GrantID {
			return current, nil
		}
	}
	return n, denied()
}
