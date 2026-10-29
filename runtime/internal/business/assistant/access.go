package assistant

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// Trusted read for ownership checks; callers authenticate and authorize the principal.
func GetGroup(ctx context.Context, store persistence.Store, owner, id core.ID) (Group, error) {
	group, err := persistence.Load[Group](ctx, store, groupBucket, persistence.Key(owner, id))
	if err == nil && group.OwnerID != owner {
		return Group{}, core.NewError(core.Forbidden, "group ownership mismatch")
	}
	return group, err
}

// ResolveActive reads a trusted caller's active assistant in the current transaction.
// Authentication and broader action authorization remain the caller's responsibility.
func ResolveActive(ctx context.Context, store persistence.Store, p core.Principal, id core.ID) (Assistant, error) {
	if id.Validate() != nil {
		return Assistant{}, core.Invalid("assistant_id", "required")
	}
	if p.Kind == core.ActorAssistant && p.SubjectID != id {
		return Assistant{}, core.NewError(core.Forbidden, "object access denied")
	}
	value, err := NewRepository(store).Get(ctx, p.OwnerID, id)
	if err != nil {
		return Assistant{}, err
	}
	if value.OwnerID != p.OwnerID || value.State != Active {
		return Assistant{}, core.NewError(core.Forbidden, "object access denied")
	}
	return value, nil
}
