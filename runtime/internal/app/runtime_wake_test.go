package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/business/watch"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func TestRuntimeWakeUsesPersistedBoundariesAndPendingExternalWork(t *testing.T) {
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "runtime.db"))
	need(t, err)
	defer store.Close()
	now := time.Now().UTC()
	next, err := nextRuntimeTick(t.Context(), store, now)
	need(t, err)
	if !next.IsZero() {
		t.Fatal("empty runtime requires polling", next)
	}
	due := now.Add(time.Hour)
	earlier := now.Add(time.Minute)
	need(t, store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, watch.Bucket, "owner/paused", watch.Watch{Paused: true, StartAt: &earlier}); err != nil {
			return err
		}
		return tx.Put(ctx, watch.Bucket, "owner/future", watch.Watch{State: "pending", StartAt: &due})
	}))
	next, err = nextRuntimeTick(t.Context(), store, now)
	need(t, err)
	if !next.Equal(due) {
		t.Fatal(next, due)
	}
	need(t, store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return tx.Put(ctx, "files.gc", "file", "file") }))
	next, err = nextRuntimeTick(t.Context(), store, now)
	need(t, err)
	if !next.Equal(now.Add(30 * time.Second)) {
		t.Fatal("pending deletion retry lost", next)
	}
}
