package app

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/textfiles"
	"molr.ai/runtime/internal/workspace"
)

type workspaceAccess struct{ ad adapters }

func newWorkspaceExplorer(ad adapters, maxUpload int64) *workspace.Explorer {
	return workspace.New(workspaceAccess{ad}.root, maxUpload)
}
func (s workspaceAccess) root(ctx context.Context, p core.Principal, id core.ID, write bool, size int64) (string, error) {
	if p.Kind != core.ActorOwner {
		return "", denied()
	}
	if err := (access{s.ad.store}).assistant(ctx, p, id); err != nil {
		return "", err
	}
	if !textfiles.Supported() {
		return "", core.WithReason(core.NewError(core.FailedPrecondition, "workspace storage unavailable"), "storage_unavailable")
	}
	if _, err := groupReference(ctx, s.ad.store, p.OwnerID, "sandbox", "local"); err != nil {
		return "", err
	}
	root := platform.AssistantWorkspace(s.ad.dataDir, s.ad.storageConfig.WorkRoot, p.OwnerID, id)
	if write {
		spec, err := workspaceConstraints(ctx, s.ad.store, p.OwnerID, id, "local")
		if err != nil {
			return "", err
		}
		if (s.ad.storageConfig.MaxFileBytes > 0 && size > s.ad.storageConfig.MaxFileBytes) || (spec != nil && size > spec.MaxFileBytes) {
			return "", core.WithReason(core.NewError(core.FailedPrecondition, "workspace file size limit exceeded"), "storage_file_limit")
		}
		if err = applyWorkspaceQuota(root, spec, s.ad.storageConfig.WorkspaceProjectQuotas); err != nil {
			return "", err
		}
	}
	if err := platform.EnsureWorkspace(root); err != nil {
		return "", err
	}
	return root, nil
}
