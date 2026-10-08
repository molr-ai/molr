package testkit

import (
	"context"
	"molr.ai/runtime/internal/persistence"
)

// ObservedStore measures storage work, independent of machine speed. Use only
// from sequential tests; setup can write to the underlying Store directly.
type ObservedStore struct {
	persistence.Store
	Scans int
	Rows  map[string]int
}
type observedTx struct {
	persistence.Tx
	store *ObservedStore
}

func (s *ObservedStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, observedTx{tx, s}) })
}
func (s *ObservedStore) Write(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, observedTx{tx, s}) })
}
func (tx observedTx) Scan(ctx context.Context, bucket string) ([]persistence.Record, error) {
	tx.store.Scans++
	return tx.Tx.Scan(ctx, bucket)
}
func (tx observedTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	rows, err := tx.Tx.Query(ctx, bucket, q)
	if tx.store.Rows == nil {
		tx.store.Rows = map[string]int{}
	}
	tx.store.Rows[bucket] += len(rows)
	return rows, err
}
