package policy

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"slices"
	"time"
)

// Project only explicitly selected, unchanged, live source grants. Reusing the
// original grant identity keeps resource accounting shared instead of cloning it.
func delegatedGrants(ctx context.Context, store persistence.Store, owner core.ID, q Query, at time.Time) ([]Grant, bool, error) {
	if q.AssistantID == "" {
		return nil, false, nil
	}
	var a struct {
		EmployerID, HiringID core.ID
		State                string
		ExpiresAt            *time.Time
		AttachedGrants       []struct {
			ID      core.ID
			Version core.Version
		}
	}
	err := store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Get(ctx, "business.assistants", persistence.Key(owner, q.AssistantID), &a)
	})
	if persistence.Missing(err) {
		return nil, false, nil
	}
	if err != nil || a.EmployerID == "" {
		return nil, false, err
	}
	out := []Grant{}
	if a.State != "active" || (a.ExpiresAt != nil && !a.ExpiresAt.After(at)) {
		return out, true, nil
	}
	var parent struct {
		State  string
		Hiring *struct {
			ID        core.ID
			Enabled   bool
			ExpiresAt *time.Time
			GrantIDs  []core.ID
		}
	}
	if err := store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Get(ctx, "business.assistants", persistence.Key(owner, a.EmployerID), &parent)
	}); err != nil {
		return nil, true, err
	}
	h := parent.Hiring
	if parent.State != "active" || h == nil || h.ID != a.HiringID || !h.Enabled || (h.ExpiresAt != nil && !h.ExpiresAt.After(at)) {
		return out, true, nil
	}
	for _, ref := range a.AttachedGrants {
		if !slices.Contains(h.GrantIDs, ref.ID) {
			continue
		}
		g, err := persistence.Load[Grant](ctx, store, "security.grants", persistence.Key(owner, ref.ID))
		if persistence.Missing(err) {
			continue
		}
		if err != nil {
			return nil, true, err
		}
		if g.OwnerID != owner || g.AssistantID != a.EmployerID || g.Version != ref.Version || g.RevokedAt != nil || (g.ExpiresAt != nil && !g.ExpiresAt.After(at)) || g.Scope.FullAccess || g.Scope.Once || (q.ResourceID != "" && g.ResourceID != q.ResourceID) {
			continue
		}
		for _, deadline := range []*time.Time{a.ExpiresAt, h.ExpiresAt} {
			if deadline != nil && (g.ExpiresAt == nil || deadline.Before(*g.ExpiresAt)) {
				g.ExpiresAt = deadline
			}
		}
		g.AssistantID = q.AssistantID
		out = append(out, g)
	}
	return out, true, nil
}
