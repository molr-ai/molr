// Package persistence defines storage-neutral, transaction-scoped persistence.
// It imports no business, IM, Agent or SQLite package.
package persistence

import (
	"context"
	"encoding/json"
	"errors"

	"molr.ai/runtime/internal/core"
)

// Record is one encoded value in a private module-owned keyspace.
type Record struct {
	Key  string
	Data json.RawMessage
}

// Tx is valid only during its callback. Keys and buckets are parameters, never
// SQL fragments or paths. Scan returns keys in ascending lexical order.
type Tx interface {
	Get(context.Context, string, string, any) error
	Put(context.Context, string, string, any) error
	Delete(context.Context, string, string) error
	Scan(context.Context, string) ([]Record, error)
	Query(context.Context, string, Query) ([]Record, error)
	Store() Store
	// NotifyAfterCommit registers a coalescible wakeup, discarded on rollback.
	NotifyAfterCommit(context.Context, SignalKey) error
}

// SignalKey identifies a process-local wakeup scope, not an event payload.
type SignalKey struct{ Namespace, Key string }

// Watch broadcasts committed changes independently to every subscriber.
// Changes coalesces notifications; Done closes on Close or backing-store shutdown.
// Close is idempotent. Callers must close their watch when finished.
type Watch interface {
	Changes() <-chan struct{}
	Done() <-chan struct{}
	Close()
}

// Store passes a transaction-bearing context to each callback. Nested calls
// using that context join the same transaction. Read cannot upgrade to Write.
// Nested writes use savepoints: failed nested changes roll back; the outer
// callback decides whether to continue or propagate the failure.
// No callback may perform external effects or retain its Tx after completion.
type Store interface {
	Watch(SignalKey) (Watch, error)
	Read(context.Context, func(context.Context, Tx) error) error
	Write(context.Context, func(context.Context, Tx) error) error
}

// Key is for validated identifiers; arbitrary user input must not form key parts.
func Key(owner core.ID, id core.ID) string { return string(owner) + "/" + string(id) }
func Missing(err error) bool               { return core.CodeOf(err) == core.NotFound }
func Conflict(message string) error        { return core.NewError(core.Conflict, message) }

// Decode is for private persisted records, never untrusted API request bodies.
func Decode[T any](record Record) (T, error) {
	var value T
	if err := json.Unmarshal(record.Data, &value); err != nil {
		return value, core.Wrap(core.Internal, "invalid stored record", err)
	}
	return value, nil
}

func Load[T any](ctx context.Context, store Store, bucket, key string) (T, error) {
	var value T
	err := store.Read(ctx, func(ctx context.Context, tx Tx) error { return tx.Get(ctx, bucket, key, &value) })
	return value, err
}
func Scan[T any](ctx context.Context, store Store, bucket string) ([]T, error) {
	var values []T
	err := store.Read(ctx, func(ctx context.Context, tx Tx) error {
		records, err := tx.Scan(ctx, bucket)
		if err != nil {
			return err
		}
		for _, record := range records {
			value, err := Decode[T](record)
			if err != nil {
				return err
			}
			values = append(values, value)
		}
		return nil
	})
	return values, err
}
func CheckContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return core.Wrap(core.DeadlineExceeded, "operation timed out", err)
		}
		return core.Wrap(core.Canceled, "operation canceled", err)
	}
	return nil
}
