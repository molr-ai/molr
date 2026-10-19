package policy

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// Trusted read for ownership checks; callers authenticate and authorize the principal.
func LoadPermissionRequest(ctx context.Context, store persistence.Store, owner, id core.ID) (PermissionRequest, error) {
	return persistence.Load[PermissionRequest](ctx, store, "security.permission_requests", persistence.Key(owner, id))
}
