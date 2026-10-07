package app

import (
	"context"
	"math"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"time"
)

func applyGroupCommand(ctx context.Context, store persistence.Store, e core.Execution, id core.ID, c *platform.Command) error {
	ref, err := groupReference(ctx, store, e.Principal.OwnerID, "sandbox", id)
	if err != nil || ref == nil {
		return err
	}
	if v := ref.Quota.Compute; v != nil {
		c.GroupComputeConstraints = v
		c.Limits.CPUPercent = min(c.Limits.CPUPercent, v.CPUPercent)
		c.Limits.MemoryBytes = min(c.Limits.MemoryBytes, v.MemoryBytes)
		c.Limits.Processes = min(c.Limits.Processes, v.Processes)
		c.Limits.TempBytes = min(c.Limits.TempBytes, v.TempBytes)
		if !v.UnlimitedDuration {
			d := time.Duration(v.MaxDurationSeconds) * time.Second
			if c.Timeout == 0 || c.Timeout > d {
				c.Timeout = d
			}
		}
	}
	return nil
}
func groupNetworkAuthorization(ctx context.Context, store persistence.Store, owner, id core.ID, n platform.NetworkAuthorization) (platform.NetworkAuthorization, error) {
	ref, err := groupReference(ctx, store, owner, "sandbox", id)
	if err != nil {
		return n, err
	}
	n.GroupSource = ""
	n.GroupConstraints = nil
	if ref != nil && ref.Quota.Network != nil {
		n.GroupSource = string(id)
		n.GroupConstraints = ref.Quota.Network
	}
	return n, nil
}
func isWorkspaceResource(ctx context.Context, store persistence.Store, owner, id, sandbox core.ID) bool {
	r, err := resource.NewRepository(store).Get(ctx, owner, id)
	return err == nil && r.Provider == "managed-workspace" && sandboxOf(r) == sandbox
}

// Storage is reserved by the hard capacities authorized to each assistant.
// Their sum is bounded by the account ceiling, so concurrent shell writes cannot
// escape the account limit. Revoked grants retain their last base capacity until
// explicitly reduced; removing permission never silently restores unlimited disk.
func groupStorageAllocation(ctx context.Context, store persistence.Store, owner, assistant, sandbox core.ID, next *policy.StorageConstraints) error {
	ref, err := groupReference(ctx, store, owner, "sandbox", sandbox)
	if err != nil || ref == nil || ref.Quota.Storage == nil {
		return err
	}
	if next == nil {
		return core.WithReason(core.NewError(core.Forbidden, "authorize workspace capacity within group limit first"), "group_storage_authorization_required")
	}
	max := ref.Quota.Storage
	if next.MaxFileBytes > max.MaxFileBytes {
		return groupQuotaExceeded()
	}
	rows, err := persistence.Select[policy.Grant](ctx, store, "security.grants", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner)}})
	if err != nil {
		return err
	}
	latest := map[core.ID]policy.Grant{}
	for _, g := range rows {
		if g.OwnerID != owner || g.AssistantID == assistant || g.Constraints == nil || g.Constraints.Storage == nil {
			continue
		}
		if !isWorkspaceResource(ctx, store, owner, g.ResourceID, sandbox) {
			continue
		}
		if old, ok := latest[g.AssistantID]; !ok || g.UpdatedAt.After(old.UpdatedAt) {
			latest[g.AssistantID] = g
		}
	}
	capacity, files := next.CapacityBytes, next.MaxFiles
	for _, g := range latest {
		limits := g.Constraints
		if g.RevokedAt == nil && (g.ExpiresAt == nil || g.ExpiresAt.After(time.Now())) {
			limits = g.EffectiveConstraints(time.Now())
		}
		v := limits.Storage
		if v.CapacityBytes > math.MaxInt64-capacity || v.MaxFiles > math.MaxInt64-files {
			return groupQuotaExceeded()
		}
		capacity += v.CapacityBytes
		files += v.MaxFiles
	}
	if capacity > max.CapacityBytes || files > max.MaxFiles {
		return groupQuotaExceeded()
	}
	return nil
}

func hasGroupNetworkQuotas(rows []platform.NetworkAuthorization) bool {
	for _, r := range rows {
		if r.GroupConstraints != nil {
			return true
		}
	}
	return false
}
