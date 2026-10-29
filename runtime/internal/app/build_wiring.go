package app

import (
	"context"
	"molr.ai/runtime/internal/accountview"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/mcpclient"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/plugins"
)

type buildWiring struct {
	validateReferences func(context.Context, core.Principal, []auth.ResourceReference) error
	plugins            *plugins.Service
	mcp                *mcpclient.Service
	resetContext       func(context.Context, core.Principal, core.ID, core.Mutation, uint64) error
	router             *sandboxRouter
}

func validateCatalogReferences(ctx context.Context, adapters adapters, sandboxRouter *sandboxRouter, executor tools.Executor, p core.Principal, refs []auth.ResourceReference) error {
	catalog, err := groupCatalog(ctx, p, adapters, sandboxRouter, executor)
	if err != nil {
		return err
	}
	available := map[string]accountview.CatalogItem{}
	for _, item := range catalog {
		available[item.Kind+":"+string(item.ID)] = item
	}
	for _, ref := range refs {
		item, ok := available[ref.Kind+":"+string(ref.ID)]
		if !ok {
			return core.Invalid("resource", "resource reference is not in the catalog")
		}
		if ref.Quota.Storage != nil && !item.WorkspaceQuotaSupported {
			return core.NewError(core.FailedPrecondition, "sandbox does not support storage hard quotas")
		}
		if ref.Quota.Network != nil && item.Limits != nil && (item.Limits.NetworkBytes == 0 || ref.Quota.Network.Bytes > item.Limits.NetworkBytes || ref.Quota.Network.Connections > item.Limits.NetworkConnections) {
			return core.Invalid("quota.network", "network quota unsupported or exceeds node ceiling")
		}
		if ref.Quota.Compute != nil && item.Limits != nil {
			if err := applyComputeConstraints(&platform.Command{}, ref.Quota.Compute, *item.Limits); err != nil {
				return err
			}
		}
	}
	return nil
}
