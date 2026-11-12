package app

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform/hostprograms"
)

func (s *sandboxRouter) hostPrograms(ctx context.Context, p core.Principal, raw json.RawMessage) (hostprograms.Snapshot, error) {
	var q struct {
		ID      core.ID
		Command string
		Refresh bool
	}
	if err := decodeSandboxArgs(raw, &q); err != nil {
		return hostprograms.Snapshot{}, err
	}
	if q.ID.Validate() != nil {
		return hostprograms.Snapshot{}, core.Invalid("sandbox_id", "required")
	}
	if err := groupFullAccess(ctx, s.adapters.store, p.OwnerID, q.ID); err != nil {
		return hostprograms.Snapshot{}, err
	}
	request := hostprograms.Request{Command: q.Command, Refresh: q.Refresh}
	if q.ID == "local" {
		if !s.adapters.storageConfig.Enabled || !s.adapters.storageConfig.AllowFullAccess {
			return hostprograms.Snapshot{}, core.NewError(core.Forbidden, "Full Access is disabled")
		}
		return s.programs.List(ctx, request)
	}
	c, err := s.connection(ctx, p, q.ID)
	if err != nil {
		return hostprograms.Snapshot{}, err
	}
	client, err := s.client(ctx, p, c)
	if err != nil {
		return hostprograms.Snapshot{}, err
	}
	return client.Programs(ctx, request)
}

func (b *remoteSandboxBackend) Programs(ctx context.Context, q hostprograms.Request) (hostprograms.Snapshot, error) {
	if !b.cfg.Sandbox.Enabled || !b.cfg.Sandbox.AllowFullAccess {
		return hostprograms.Snapshot{}, core.NewError(core.Forbidden, "Full Access is disabled")
	}
	return b.programs.List(ctx, q)
}
