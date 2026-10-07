package app

import (
	"context"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/resourcequota"
	sm "molr.ai/runtime/internal/sandboxmanager"
	"os"
	"slices"
	"time"
)

func workspaceResourceID(owner, sandbox core.ID) core.ID {
	return core.ID("workspace_" + digestBytes([]byte(string(owner) + "/" + string(sandbox)))[:24])
}
func ensureWorkspaceResource(ctx context.Context, store persistence.Store, owner, sandbox core.ID) error {
	catalogOwner, err := persistence.CatalogOwner(ctx, store, owner)
	if err != nil {
		return err
	}
	owner = catalogOwner
	id := workspaceResourceID(owner, sandbox)
	repo := resource.NewRepository(store)
	if _, err := repo.Get(ctx, owner, id); err == nil {
		return nil
	} else if !persistence.Missing(err) {
		return err
	}
	return repo.Insert(ctx, resource.Resource{ID: id, OwnerID: owner, SandboxID: sandbox, Kind: resource.Storage, Provider: "managed-workspace", Name: "Assistant workspace", Actions: []string{"read", "write"}, State: resource.Available, Version: 1}, resource.Binding{ResourceID: id, SandboxID: sandbox, SandboxResource: "workspace"})
}
func workspaceConstraints(ctx context.Context, store persistence.Store, owner, assistant, sandbox core.ID) (*policy.StorageConstraints, error) {
	rows, err := persistence.Select[policy.Grant](ctx, store, "security.grants", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner), persistence.Eq("AssistantID", assistant)}})
	if err != nil {
		return nil, err
	}
	catalogOwner, err := persistence.CatalogOwner(ctx, store, owner)
	if err != nil {
		return nil, err
	}
	id := workspaceResourceID(catalogOwner, sandbox)
	// Preserve the last authorized base capacity after expiry/revocation. Existing
	// files are never erased and revoking an expansion cannot restore unlimited space.
	var selected *policy.Grant
	active := func(g *policy.Grant) bool {
		return g != nil && g.RevokedAt == nil && (g.ExpiresAt == nil || time.Now().Before(*g.ExpiresAt))
	}
	for i := range rows {
		g := &rows[i]
		if g.OwnerID != owner || g.AssistantID != assistant || g.ResourceID != id || g.Constraints == nil || g.Constraints.Storage == nil || !slices.Contains(g.Actions, policy.Write) {
			continue
		}
		if selected == nil || (active(g) && !active(selected)) || (active(g) == active(selected) && g.UpdatedAt.After(selected.UpdatedAt)) {
			selected = g
		}
	}
	if selected == nil {
		return nil, groupStorageAllocation(ctx, store, owner, assistant, sandbox, nil)
	}
	if selected.RevokedAt != nil || (selected.ExpiresAt != nil && !time.Now().Before(*selected.ExpiresAt)) {
		return selected.Constraints.Storage, groupStorageAllocation(ctx, store, owner, assistant, sandbox, selected.Constraints.Storage)
	}
	v := selected.EffectiveConstraints(time.Now()).Storage
	return v, groupStorageAllocation(ctx, store, owner, assistant, sandbox, v)
}
func applyWorkspaceQuota(root string, spec *policy.StorageConstraints, enabled bool) error {
	if spec == nil {
		return nil
	}
	if !enabled {
		return core.WithReason(core.NewError(core.FailedPrecondition, "workspace hard quotas are not configured"), "workspace_quota_unavailable")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	if _, err := resourcequota.ApplyWorkspace(root, *spec); err != nil {
		return core.WithReason(core.Wrap(core.FailedPrecondition, "workspace requires a provisioned project quota", err), "workspace_quota_unavailable")
	}
	return nil
}
func (s *shellService) workspaceQuota(ctx context.Context, e core.Execution) (*policy.StorageConstraints, error) {
	spec, err := workspaceConstraints(ctx, s.store, e.Principal.OwnerID, e.AssistantID, "local")
	if err != nil {
		return nil, err
	}
	return spec, applyWorkspaceQuota(platform.AssistantWorkspace(s.dataDir, s.config.WorkRoot, e.Principal.OwnerID, e.AssistantID), spec, s.config.WorkspaceProjectQuotas)
}

func (f *resourceFileTools) checkWorkspaceFileSize(ctx context.Context, e core.Execution, target tools.FileTarget, size int64) error {
	if target.FullAccess {
		return nil
	}
	if q, ok := ctx.Value(sandboxRequestKey{}).(sm.Request); ok && q.FullAccess {
		return nil
	}
	var spec *policy.StorageConstraints
	if _, ok := ctx.Value(sandboxRequestKey{}).(sm.Request); ok {
		// Remote file size is bounded by the operation and deployment; no workspace quota.
	} else if target.ResourceID == "" || isWorkspaceResource(ctx, f.adapters.store, e.Principal.OwnerID, target.ResourceID, "local") {
		var err error
		spec, err = workspaceConstraints(ctx, f.adapters.store, e.Principal.OwnerID, e.AssistantID, "local")
		if err != nil {
			return err
		}
	}
	if spec != nil && size > spec.MaxFileBytes {
		return core.WithReason(core.NewError(core.FailedPrecondition, "workspace single-file quota exceeded"), "storage_file_limit")
	}
	return nil
}
