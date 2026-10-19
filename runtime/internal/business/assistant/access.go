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
