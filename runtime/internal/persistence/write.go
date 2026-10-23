package persistence

import (
	"context"
	"encoding/json"

	"molr.ai/runtime/internal/core"
)

func writeConflict() error { return Conflict("version or idempotency conflict") }

// Insert stores a new record without replacing an existing key. Nested calls
// join the caller's transaction using the store's existing savepoint contract.
func Insert(ctx context.Context, store Store, bucket, key string, value any) error {
	return store.Write(ctx, func(ctx context.Context, tx Tx) error {
		var old json.RawMessage
		err := tx.Get(ctx, bucket, key, &old)
		if err == nil {
			return writeConflict()
		}
		if !Missing(err) {
			return err
		}
		return tx.Put(ctx, bucket, key, value)
	})
}

// SaveVersion replaces a record only when its current version matches expected
// and the supplied value advances that version by exactly one.
func SaveVersion[T any](ctx context.Context, store Store, bucket, key string, value T, expected core.Version, version func(T) core.Version) error {
	return store.Write(ctx, func(ctx context.Context, tx Tx) error {
		var old T
		if err := tx.Get(ctx, bucket, key, &old); err != nil {
			return err
		}
		if expected == 0 || version(old) != expected || version(value) != expected+1 {
			return writeConflict()
		}
		return tx.Put(ctx, bucket, key, value)
	})
}

// DeleteVersion removes a record only when its current version matches expected.
func DeleteVersion[T any](ctx context.Context, store Store, bucket, key string, expected core.Version, version func(T) core.Version) error {
	return store.Write(ctx, func(ctx context.Context, tx Tx) error {
		var old T
		if err := tx.Get(ctx, bucket, key, &old); err != nil {
			return err
		}
		if expected == 0 || version(old) != expected {
			return writeConflict()
		}
		return tx.Delete(ctx, bucket, key)
	})
}
