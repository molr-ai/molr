package resource

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

func managedConnection(r Resource) bool {
	return r.Provider != "managed-workspace" && (r.Kind == Storage || r.Kind == Network || r.Kind == Compute)
}

func (s *service) Reconnect(ctx context.Context, p core.Principal, req ReconnectRequest) (Resource, error) {
	if err := req.Mutation.Validate(true); err != nil {
		return Resource{}, err
	}
	var snapshot Resource
	var binding Binding
	err := s.read(ctx, p, req.ID, policy.Manage, true, func(ctx context.Context) error {
		var err error
		snapshot, err = s.repo.Get(ctx, p.OwnerID, req.ID)
		if err != nil {
			return err
		}
		if !managedConnection(snapshot) {
			return forbidden()
		}
		binding, err = s.repo.Binding(ctx, p.OwnerID, req.ID)
		return err
	})
	if err != nil {
		return Resource{}, err
	}
	// A successful replay needs no external probe. The mutation receipt below
	// distinguishes it from a stale/new request against an already connected row.
	if snapshot.State == Disconnected {
		if snapshot.Version != req.Mutation.ExpectedVersion {
			return Resource{}, conflict()
		}
		if s.options.CheckResource == nil {
			return Resource{}, core.NewError(core.Unavailable, "resource checking unavailable")
		}
		status, err := s.options.CheckResource(ctx, p, snapshot, binding)
		if err != nil {
			return Resource{}, err
		}
		if status.State != Available {
			return Resource{}, core.WithReason(core.NewError(core.FailedPrecondition, "resource is unavailable"), core.Reason(status.ReasonCode))
		}
	}
	return mutate(ctx, s, p, "reconnect", req.ID, req.Mutation, req, true, true, func(ctx context.Context) (Resource, error) {
		current, err := s.repo.Get(ctx, p.OwnerID, req.ID)
		if err != nil {
			return current, err
		}
		if current.Version != snapshot.Version || current.Version != req.Mutation.ExpectedVersion {
			return current, conflict()
		}
		if !managedConnection(current) || current.State != Disconnected {
			return current, core.NewError(core.FailedPrecondition, "only disconnected resources can reconnect")
		}
		current.State, current.Reason = Available, ""
		current.Version++
		return current, s.repo.Save(ctx, current, req.Mutation.ExpectedVersion)
	})
}

func (s *service) Delete(ctx context.Context, p core.Principal, req DeleteRequest) error {
	// Collection authorization allows replay after the resource has been removed.
	_, err := mutateForTarget(ctx, s, p, "delete", req.ID, req.Mutation, req, true, true, core.Object{Kind: objectKind}, func(ctx context.Context) (struct{}, error) {
		result := struct{}{}
		r, err := s.repo.Get(ctx, p.OwnerID, req.ID)
		if err != nil {
			return result, err
		}
		if r.Version != req.Mutation.ExpectedVersion {
			return result, conflict()
		}
		if !managedConnection(r) {
			return result, forbidden()
		}
		if r.State != Disconnected {
			return result, core.WithReason(core.NewError(core.FailedPrecondition, "disconnect the resource before deleting"), "resource_not_disconnected")
		}
		// The catalog is shared. Grants from every account block deletion, including
		// grants held by inactive assistants; history alone never blocks deletion.
		grants, err := persistence.Select[policy.Grant](ctx, s.store, "security.grants", persistence.Query{
			Where: []persistence.Filter{persistence.Eq("ResourceID", r.ID), persistence.Eq("RevokedAt", nil)},
			Any:   [][]persistence.Filter{{persistence.Eq("ExpiresAtKey", "")}, {{Field: "ExpiresAtKey", Op: ">", Value: persistence.TimeKey(s.now())}}}, Limit: 1,
		})
		if err != nil {
			return result, err
		}
		if len(grants) != 0 {
			return result, core.WithReason(core.NewError(core.FailedPrecondition, "revoke active grants before deleting"), "resource_has_grants")
		}
		err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			if err := tx.Delete(ctx, bindingBucket, persistence.Key(r.OwnerID, r.ID)); err != nil {
				return err
			}
			return tx.Delete(ctx, bucket, persistence.Key(r.OwnerID, r.ID))
		})
		return result, err
	})
	return err
}
