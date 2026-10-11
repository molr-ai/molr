package app

import (
	"context"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/quota"
)

func catalogOwned(ctx context.Context, store persistence.Store, actual, caller core.ID) bool {
	owner, err := persistence.CatalogOwner(ctx, store, caller)
	return err == nil && actual == owner
}

func groupPolicy(ctx context.Context, store persistence.Store, owner core.ID) (auth.UserGroup, error) {
	// Standalone module stores have no accounts. Runtime always authenticates
	// first; retaining this mode keeps the modules independently composable.
	if _, err := persistence.Load[struct{ AccountID core.ID }](ctx, store, "auth.bootstrap", "owner"); persistence.Missing(err) {
		return auth.UserGroup{Administrator: true}, nil
	} else if err != nil {
		return auth.UserGroup{}, err
	}
	return auth.GroupFor(ctx, store, owner)
}
func groupReference(ctx context.Context, store persistence.Store, owner core.ID, kind string, id core.ID) (*auth.ResourceReference, error) {
	g, err := groupPolicy(ctx, store, owner)
	if err != nil {
		return nil, err
	}
	if g.Administrator {
		return nil, nil
	}
	for _, ref := range g.Resources {
		if ref.Kind == kind && ref.ID == id {
			return &ref, nil
		}
	}
	return nil, core.WithReason(core.NewError(core.Forbidden, "resource unavailable for user group"), "group_resource_denied")
}
func groupAccess(ctx context.Context, store persistence.Store, p core.Principal, q policy.CheckRequest) error {
	g, err := groupPolicy(ctx, store, p.OwnerID)
	if err != nil {
		return err
	}
	if g.Administrator {
		return nil
	}
	if q.Target.Kind == "resource" && (q.Action == policy.Manage || q.Action == policy.Write) && p.Kind == core.ActorOwner {
		return core.NewError(core.Forbidden, "resource configuration requires administrator")
	}
	if q.Target.Kind == "tool" && q.Target.ID != "" && q.Target.ID != "catalog" {
		_, err := groupReference(ctx, store, p.OwnerID, "tool", q.Target.ID)
		return err
	}
	id := q.ResourceID
	if id == "" && q.Target.Kind == "resource" {
		id = q.Target.ID
	}
	if id == "" {
		return nil
	}
	r, err := resource.NewRepository(store).Get(ctx, p.OwnerID, id)
	if err != nil {
		return err
	}
	if r.Kind == resource.Model {
		b, err := resource.NewRepository(store).Binding(ctx, p.OwnerID, id)
		if err != nil {
			return err
		}
		for _, entry := range modelEntries(r, b) {
			if q.ModelID != "" && q.ModelID != entry.id {
				continue
			}
			if _, err := groupReference(ctx, store, p.OwnerID, "model", entry.id); err == nil {
				return nil
			} else if core.CodeOf(err) != core.Forbidden {
				return err
			}
		}
		return core.WithReason(core.NewError(core.Forbidden, "model unavailable for user group"), "group_resource_denied")
	}
	if r.Kind == resource.Connection {
		_, err := groupReference(ctx, store, p.OwnerID, "connection", r.ID)
		return err
	}
	if r.Kind == resource.Tool {
		_, err := groupReference(ctx, store, p.OwnerID, "tool", r.ID)
		return err
	}
	if q.FullAccess {
		return groupFullAccess(ctx, store, p.OwnerID, sandboxOf(r))
	}
	_, err = groupReference(ctx, store, p.OwnerID, "sandbox", sandboxOf(r))
	return err
}

func validateGroupConstraints(ctx context.Context, store persistence.Store, p core.Principal, id, assistantID core.ID, c *policy.Constraints) error {
	if err := groupAccess(ctx, store, p, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: id}, ResourceID: id, Action: policy.Read}); err != nil {
		return err
	}
	if c == nil {
		return nil
	}
	r, err := resource.NewRepository(store).Get(ctx, p.OwnerID, id)
	if err != nil {
		return err
	}
	ref, err := groupReference(ctx, store, p.OwnerID, "sandbox", sandboxOf(r))
	if err != nil || ref == nil {
		return err
	}
	if c.Storage != nil {
		if err := groupStorageAllocation(ctx, store, p.OwnerID, assistantID, sandboxOf(r), c.Storage); err != nil {
			return err
		}
	}
	q := ref.Quota
	if c.Compute != nil {
		if err := core.CheckGPUDevices(c.Compute.GPUDevices(), q.Compute.GPUDevices()); err != nil {
			return err
		}
	}
	if v, max := c.Compute, q.Compute; v != nil && max != nil {
		if v.CPUPercent > max.CPUPercent || v.MemoryBytes > max.MemoryBytes || v.Processes > max.Processes || v.TempBytes > max.TempBytes || (!max.UnlimitedDuration && (v.UnlimitedDuration || v.MaxDurationSeconds > max.MaxDurationSeconds)) {
			return groupQuotaExceeded()
		}
	}
	if v, max := c.Network, q.Network; v != nil && max != nil {
		if v.Bytes > max.Bytes || v.Connections > max.Connections {
			return groupQuotaExceeded()
		}
	}
	if v, max := c.Storage, q.Storage; v != nil && max != nil {
		if v.CapacityBytes > max.CapacityBytes || v.MaxFileBytes > max.MaxFileBytes || v.MaxFiles > max.MaxFiles {
			return groupQuotaExceeded()
		}
	}
	return nil
}
func groupQuotaExceeded() error {
	return quota.Exceeded()
}

// Full Access requires explicit administrator delegation to ordinary members.
func groupFullAccess(ctx context.Context, store persistence.Store, owner, sandbox core.ID) error {
	ref, err := groupReference(ctx, store, owner, "sandbox", sandbox)
	if err != nil {
		return err
	}
	if ref != nil && !ref.AllowFullAccess {
		return core.WithReason(core.NewError(core.Forbidden, "Full Access unavailable for user group"), "group_full_access_denied")
	}
	return nil
}
