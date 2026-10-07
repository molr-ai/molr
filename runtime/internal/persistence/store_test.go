package persistence_test

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"path/filepath"
	"testing"
	"time"
)

func TestPersistenceHelpersOnRealTransactions(t *testing.T) {
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := t.Context()
	key := persistence.Key("owner", "item")
	if key != "owner/item" {
		t.Fatal(key)
	}
	if err = store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "sample", key, 7); err != nil {
			return err
		}
		var value int
		if err := tx.Get(ctx, "sample", key, &value); err != nil || value != 7 {
			t.Fatalf("read own write: %d %v", value, err)
		}
		return tx.Store().Write(ctx, func(ctx context.Context, nested persistence.Tx) error {
			return nested.Put(ctx, "sample", "owner/second", 8)
		})
	}); err != nil {
		t.Fatal(err)
	}
	value, err := persistence.Load[int](ctx, store, "sample", key)
	if err != nil || value != 7 {
		t.Fatal(value, err)
	}
	values, err := persistence.Scan[int](ctx, store, "sample")
	if err != nil || len(values) != 2 {
		t.Fatal(values, err)
	}
	if err = store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		rows, err := tx.Scan(ctx, "sample")
		if err != nil || len(rows) != 2 {
			t.Fatal(rows, err)
		}
		return tx.Delete(ctx, "sample", key)
	}); err != nil {
		t.Fatal(err)
	}
	_, err = persistence.Load[int](ctx, store, "sample", key)
	if !persistence.Missing(err) {
		t.Fatal(err)
	}
	if _, err = persistence.Decode[int](persistence.Record{Data: []byte(`"bad"`)}); core.CodeOf(err) != core.Internal {
		t.Fatal(err)
	}
	if core.CodeOf(persistence.Conflict("conflict")) != core.Conflict {
		t.Fatal("wrong error classification")
	}
	if persistence.CheckContext(ctx) != nil {
		t.Fatal("live context rejected")
	}
	c, cancel := context.WithCancel(ctx)
	cancel()
	if core.CodeOf(persistence.CheckContext(c)) != core.Canceled {
		t.Fatal("cancellation lost")
	}
	d, stop := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer stop()
	if core.CodeOf(persistence.CheckContext(d)) != core.DeadlineExceeded {
		t.Fatal("deadline lost")
	}
}

func TestSharedCatalogKeepsPrivateNamespaces(t *testing.T) {
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := t.Context()
	if owner, err := persistence.CatalogOwner(ctx, store, "member"); err != nil || owner != "member" {
		t.Fatal(owner, err)
	}
	err = store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		for _, r := range []struct {
			bucket, key string
			value       any
		}{{"system.catalog", "enabled", true}, {"auth.bootstrap", "owner", struct{ AccountID core.ID }{"original"}}, {"resources", "original/model", "shared"}, {"private", "original/message", "private"}} {
			if err := tx.Put(ctx, r.bucket, r.key, r.value); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if owner, err := persistence.CatalogOwner(ctx, store, "member"); err != nil || owner != "original" {
		t.Fatal(owner, err)
	}
	if v, err := persistence.LoadCatalog[string](ctx, store, "resources", "member", "model"); err != nil || v != "shared" {
		t.Fatal(v, err)
	}
	if _, err := persistence.Load[string](ctx, store, "private", persistence.Key("member", "message")); !persistence.Missing(err) {
		t.Fatal("private data namespace changed", err)
	}
}
