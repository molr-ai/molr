package plan

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

// Read uses the caller's real identity, including assistant assignment checks.
func (s *Coordinator) Read(ctx context.Context, p core.Principal, ref core.Object) (Plan, error) {
	var out Plan
	err := s.store.Read(ctx, func(ctx context.Context, _ persistence.Tx) error {
		if err := s.authorize(ctx, p, ref, policy.Read); err != nil {
			return err
		}
		var err error
		out, err = s.source(ctx, p.OwnerID, ref)
		if err != nil {
			return err
		}
		out.Executions, err = s.runs(ctx, p.OwnerID, out)
		if err != nil {
			return err
		}
		out.BlockedBy, err = s.ready(ctx, p.OwnerID, out)
		out.Ready = len(out.BlockedBy) == 0 && !out.Links.Paused
		return err
	})
	return out, err
}
