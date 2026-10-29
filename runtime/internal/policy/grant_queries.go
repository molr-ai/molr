package policy

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"time"
)

// ActiveGrants reads current candidates in the caller's transaction. It is an
// internal repository read, not authorization: callers still check action,
// scope, resource availability and execution identity before use.
func ActiveGrants(ctx context.Context, store persistence.Store, owner core.ID, q Query, at time.Time) ([]Grant, error) {
	filters := []persistence.Filter{persistence.Eq("OwnerID", owner), persistence.Eq("RevokedAt", nil)}
	if q.AssistantID != "" {
		filters = append(filters, persistence.Eq("AssistantID", q.AssistantID))
	}
	if q.ResourceID != "" {
		filters = append(filters, persistence.Eq("ResourceID", q.ResourceID))
	}
	return persistence.Select[Grant](ctx, store, "security.grants", persistence.Query{Where: filters, Any: [][]persistence.Filter{{persistence.Eq("ExpiresAtKey", "")}, {{Field: "ExpiresAtKey", Op: ">", Value: persistence.TimeKey(at)}}}})
}
