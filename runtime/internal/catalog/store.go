// Package catalog owns the installation-wide resource namespace. User-owned
// grants, executions, usage and files remain in their separate account namespaces.
package catalog

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// Scope is an installation-local namespace, never an authenticated account.
const Scope core.ID = "instance-catalog"

// Load reads a shared resource. Callers must authorize the requesting principal.
func Load[T any](ctx context.Context, store persistence.Store, bucket string, id core.ID) (T, error) {
	return persistence.Load[T](ctx, store, bucket, persistence.Key(Scope, id))
}

// Select restricts a shared-resource query to the installation namespace.
func Select[T any](ctx context.Context, store persistence.Store, bucket string, q persistence.Query) ([]T, error) {
	q.Where = append(q.Where, persistence.Eq("OwnerID", Scope))
	return persistence.Select[T](ctx, store, bucket, q)
}
