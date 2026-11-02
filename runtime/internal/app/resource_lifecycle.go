package app

import (
	"context"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/catalog"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	sm "molr.ai/runtime/internal/sandboxmanager"
	"molr.ai/runtime/internal/textfiles"
	"slices"
	"time"
)

func (s *sandboxRouter) checkResource(ctx context.Context, p core.Principal, r resource.Resource, b resource.Binding) (resource.Status, error) {
	status := resource.Status{State: resource.Available, ReasonCode: "available", CheckedAt: time.Now()}
	unavailable := func(reason string) (resource.Status, error) {
		if err := ctx.Err(); err != nil {
			return resource.Status{}, err
		}
		status.State, status.ReasonCode = resource.Offline, reason
		return status, nil
	}
	if r.Provider == "managed-workspace" {
		if !textfiles.Supported() {
			return unavailable("storage_unavailable")
		}
		return status, nil
	}
	if sandboxOf(r) == "local" && r.Kind == resource.Storage {
		actions, err := s.adapters.providers()["local-directory"].Validate(ctx, b)
		if err != nil {
			reason := string(core.ReasonOf(err))
			if reason == "" {
				reason = "directory_unavailable"
			}
			return unavailable(reason)
		}
		if slices.Contains(r.Actions, "write") && !slices.Contains(actions, "write") {
			return unavailable("storage_read_only")
		}
		return status, nil
	}
	info, err := s.info(ctx, p, sandboxOf(r))
	if err != nil || !info.Enabled {
		return unavailable("sandbox_offline")
	}
	switch r.Kind {
	case resource.Compute:
		if !info.Capabilities.Shell && !info.Capabilities.FullAccess {
			return unavailable("sandbox_compute_unavailable")
		}
	case resource.Network:
		target := b.SandboxResource
		if sandboxOf(r) == "local" {
			target = b.SandboxTarget
			if !s.adapters.storageConfig.Enabled {
				return unavailable("sandbox_disabled")
			}
		}
		if !slices.ContainsFunc(info.Networks, func(n sm.Network) bool { return n.ID == target }) {
			return unavailable("network_target_unavailable")
		}
	case resource.Storage:
		if !info.Storage {
			return unavailable("storage_unavailable")
		}
		ref, err := directoryReference(b.SandboxResource)
		if err != nil {
			return unavailable("directory_unavailable")
		}
		conn, err := s.connection(ctx, p, sandboxOf(r))
		if err != nil {
			return unavailable("sandbox_offline")
		}
		client, err := s.client(ctx, p, conn)
		if err != nil {
			return unavailable("sandbox_offline")
		}
		dir, err := client.Directory(ctx, ref)
		if err != nil {
			return unavailable("directory_unavailable")
		}
		if dir.ReadOnly && slices.Contains(r.Actions, "write") {
			return unavailable("storage_read_only")
		}
	default:
		return unavailable("resource_unavailable")
	}
	return status, nil
}

// One-shot checks only: disconnected resources are never probed or recreated.
func refreshSandboxResources(ctx context.Context, store persistence.Store, service resource.Service, p core.Principal, sandbox core.ID) error {
	rows, err := catalog.Select[resource.Resource](ctx, store, "business.resources", persistence.Query{})
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.State == resource.Disconnected || (sandbox != "" && sandboxOf(r) != sandbox) || (r.Kind != resource.Storage && r.Kind != resource.Network && r.Kind != resource.Compute) {
			continue
		}
		if _, err := service.Check(ctx, p, r.ID); err != nil && !persistence.Missing(err) {
			return err
		}
	}
	return nil
}

func (b *Bundle) refreshStartupResources(ctx context.Context) error {
	rows, err := catalog.Select[resource.Resource](ctx, b.store, "business.resources", persistence.Query{})
	if err != nil {
		return err
	}
	repo := resource.NewRepository(b.store)
	for _, r := range rows {
		if r.State == resource.Disconnected || (r.Kind != resource.Storage && r.Kind != resource.Network && r.Kind != resource.Compute) {
			continue
		}
		binding, err := repo.Binding(ctx, catalog.Scope, r.ID)
		if err != nil {
			return err
		}
		status, err := b.remote.checkResource(ctx, core.Principal{}, r, binding)
		if err != nil {
			return err
		}
		err = b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			current, err := repo.Get(ctx, catalog.Scope, r.ID)
			if persistence.Missing(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if current.Version != r.Version || current.State == resource.Disconnected {
				return nil
			}
			reason := status.ReasonCode
			if status.State == resource.Available {
				reason = ""
			}
			if current.State == status.State && current.Reason == reason {
				return nil
			}
			current.State, current.Reason = status.State, reason
			current.Version++
			return repo.Save(ctx, current, r.Version)
		})
		if err != nil {
			return err
		}
	}
	return nil
}
