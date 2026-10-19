package search

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// Trusted read for ownership checks; callers authenticate and authorize the principal.
func LoadStatus(ctx context.Context, store persistence.Store, owner, id core.ID) (IndexStatus, error) {
	return persistence.Load[IndexStatus](ctx, store, statusBucket, persistence.Key(owner, id))
}
