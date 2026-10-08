package persistence

import (
	"context"
	"molr.ai/runtime/internal/core"
)

// CatalogOwner identifies the installation-wide resource namespace. Existing
// installations keep their original prefix and encryption binding. This helper
// never applies to private business data, grants, usage, files or executions.
// A store without bootstrap metadata retains the standalone repository contract.
func CatalogOwner(ctx context.Context, store Store, fallback core.ID) (core.ID, error) {
	enabled, err := Load[bool](ctx, store, "system.catalog", "enabled")
	if Missing(err) {
		return fallback, nil
	}
	if err != nil {
		return "", err
	}
	if !enabled {
		return fallback, nil
	}
	row, err := Load[struct{ AccountID core.ID }](ctx, store, "auth.bootstrap", "owner")
	if Missing(err) {
		return fallback, nil
	}
	if err != nil {
		return "", err
	}
	if err := row.AccountID.Validate(); err != nil {
		return "", err
	}
	return row.AccountID, nil
}

func LoadCatalog[T any](ctx context.Context, store Store, bucket string, owner, id core.ID) (T, error) {
	var zero T
	owner, err := CatalogOwner(ctx, store, owner)
	if err != nil {
		return zero, err
	}
	return Load[T](ctx, store, bucket, Key(owner, id))
}

// SelectCatalog binds shared catalog reads to the installation's resource owner.
func SelectCatalog[T any](ctx context.Context, store Store, bucket string, caller core.ID, q Query) ([]T, error) {
	owner, err := CatalogOwner(ctx, store, caller)
	if err != nil {
		return nil, err
	}
	q.Where = append(q.Where, Eq("OwnerID", owner))
	return Select[T](ctx, store, bucket, q)
}
