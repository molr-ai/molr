package app

import (
	"context"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"slices"
	"sort"
	"time"
)

func executionGrant(ctx context.Context, store persistence.Store, e core.Execution, id core.ID) (policy.Grant, error) {
	rows, err := persistence.Select[policy.Grant](ctx, store, "security.grants", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", e.Principal.OwnerID), persistence.Eq("AssistantID", e.AssistantID)}})
	if err != nil {
		return policy.Grant{}, err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	for _, g := range rows {
		if g.OwnerID == e.Principal.OwnerID && g.AssistantID == e.AssistantID && g.ResourceID == id && !g.Scope.FullAccess && g.Constraints != nil && g.RevokedAt == nil && (g.ExpiresAt == nil || g.ExpiresAt.After(time.Now())) && slices.Contains(g.Actions, policy.Execute) {
			return g, nil
		}
	}
	return policy.Grant{}, denied()
}
func automaticNetworks(ctx context.Context, store persistence.Store, e core.Execution, sandboxID core.ID) ([]core.ID, error) {
	rows, err := persistence.Select[policy.Grant](ctx, store, "security.grants", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", e.Principal.OwnerID), persistence.Eq("AssistantID", e.AssistantID)}})
	if err != nil {
		return nil, err
	}
	type choice struct {
		id      core.ID
		updated time.Time
	}
	selected := map[string]choice{}
	repo := resource.NewRepository(store)
	for _, g := range rows {
		if g.OwnerID != e.Principal.OwnerID || g.AssistantID != e.AssistantID || g.Constraints == nil || g.RevokedAt != nil || (g.ExpiresAt != nil && !g.ExpiresAt.After(time.Now())) || !slices.Contains(g.Actions, policy.Execute) {
			continue
		}
		r, err := repo.Get(ctx, e.Principal.OwnerID, g.ResourceID)
		if persistence.Missing(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if r.Kind != resource.Network || r.State != resource.Available || sandboxOf(r) != sandboxID {
			continue
		}
		binding, err := repo.Binding(ctx, e.Principal.OwnerID, r.ID)
		if err != nil {
			return nil, err
		}
		key := binding.SandboxResource + "\x00" + binding.SandboxTarget
		previous, exists := selected[key]
		if !exists || g.UpdatedAt.After(previous.updated) || (g.UpdatedAt.Equal(previous.updated) && r.ID < previous.id) {
			selected[key] = choice{r.ID, g.UpdatedAt}
		}
	}
	ids := []core.ID{}
	for _, item := range selected {
		ids = append(ids, item.id)
	}
	slices.Sort(ids)
	return ids, nil
}
func applyComputeConstraints(c *platform.Command, v *policy.ComputeConstraints, max platform.Limits) error {
	if err := (&policy.Constraints{Compute: v}).Validate(); err != nil {
		return err
	}
	if v.CPUPercent > max.CPUPercent || v.MemoryBytes > max.MemoryBytes || v.Processes > max.Processes || v.TempBytes > max.TempBytes {
		return core.Invalid("constraints.compute", "requested capacity exceeds deployment ceiling")
	}
	c.ComputeQuota = true
	c.Limits.CPUPercent = v.CPUPercent
	c.Limits.MemoryBytes = v.MemoryBytes
	c.Limits.Processes = v.Processes
	c.Limits.TempBytes = v.TempBytes
	if !v.UnlimitedDuration {
		duration := time.Duration(v.MaxDurationSeconds) * time.Second
		if c.Timeout == 0 || c.Timeout > duration {
			c.Timeout = duration
		}
	}
	return nil
}
func hasNetworkQuotas(rows []platform.NetworkAuthorization) bool {
	for _, r := range rows {
		if r.Constraints != nil {
			return true
		}
	}
	return false
}
func validateResourceConstraints(ctx context.Context, store persistence.Store, p core.Principal, id core.ID, c *policy.Constraints, max platform.Limits, workspaceEnabled bool) error {
	if err := c.Validate(); err != nil {
		return err
	}
	repo := resource.NewRepository(store)
	r, err := repo.Get(ctx, p.OwnerID, id)
	if err != nil {
		return err
	}
	if c == nil {
		if r.Kind == resource.Compute || r.Kind == resource.Network || (r.Kind == resource.Storage && r.Provider == "managed-workspace") {
			return core.Invalid("constraints", "explicit resource quotas required")
		}
		return nil
	}
	if sandboxOf(r) != "local" {
		connection, err := persistence.LoadCatalog[sandboxConnection](ctx, store, "sandbox.connections", p.OwnerID, sandboxOf(r))
		if err != nil {
			return err
		}
		if !connection.View.Info.ResourceQuotas {
			return core.NewError(core.FailedPrecondition, "upgrade remote sandbox for resource quotas")
		}
		max = connection.View.Info.AuthorizationCeilings
		workspaceEnabled = connection.View.Info.WorkspaceProjectQuotas
	}
	if string(r.Kind) != c.Kind() {
		return core.Invalid("constraints", "resource kind mismatch")
	}
	if c.Compute != nil {
		return applyComputeConstraints(&platform.Command{}, c.Compute, max)
	}
	if c.Network != nil && (c.Network.Bytes > max.NetworkBytes || c.Network.Connections > max.NetworkConnections) {
		return core.Invalid("constraints.network", "requested quota exceeds deployment ceiling")
	}
	if c.Storage != nil {
		if c.Storage.MaxFileBytes > max.MaxFileBytes {
			return core.Invalid("constraints.storage", "single file limit exceeds deployment ceiling")
		}
		if r.Provider != "managed-workspace" || !workspaceEnabled {
			return core.NewError(core.FailedPrecondition, "storage resource requires a configured hard-quota backend")
		}
	}
	return nil
}
