package app

import (
	"context"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"time"
)

func (a adapters) deleteProviderReferences(ctx context.Context, p core.Principal, r resource.Resource, binding resource.Binding) error {
	grants := policy.NewRepository(a.store)
	query := policy.Query{ResourceID: r.ID, Page: core.PageRequest{Limit: 200}}
	for {
		page, err := grants.Grants(ctx, p.OwnerID, query)
		if err != nil {
			return err
		}
		for _, grant := range page.Items {
			if grant.RevokedAt == nil {
				if err := grants.RevokeGrant(ctx, p.OwnerID, grant.ID, grant.Version, time.Now().UTC()); err != nil {
					return err
				}
			}
		}
		if page.NextCursor == "" {
			break
		}
		query.Page.Cursor = page.NextCursor
	}
	preferences := settings.NewRepository(a.store)
	current, err := preferences.Get(ctx, p.OwnerID)
	if persistence.Missing(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range modelEntries(r, binding) {
		if current.DefaultModelID == entry.id {
			expected := current.Version
			current.DefaultModelID = ""
			current.Version++
			return preferences.Save(ctx, current, expected)
		}
	}
	// Explicit assistant model IDs remain unavailable until the owner selects a
	// replacement; deletion must not silently route conversations to another provider.
	return nil
}
