package catalog_test

import (
	"context"
	"molr.ai/runtime/internal/catalog"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"path/filepath"
	"testing"
)

func TestCatalogScopeIndependentOfAccounts(t *testing.T) {
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	type row struct {
		OwnerID core.ID
		ID      string
	}
	for _, bootstrap := range []core.ID{"", "admin", "replacement"} {
		err = store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			if err := tx.Put(ctx, "auth.bootstrap", "owner", struct{ AccountID core.ID }{bootstrap}); err != nil {
				return err
			}
			for _, owner := range []core.ID{catalog.Scope, "admin", "member"} {
				if err := tx.Put(ctx, "resources", persistence.Key(owner, "model"), row{owner, "model"}); err != nil {
					return err
				}
				if err := tx.Put(ctx, "private", persistence.Key(owner, "message"), row{owner, "message"}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		got, err := catalog.Load[row](t.Context(), store, "resources", "model")
		if err != nil || got.OwnerID != catalog.Scope {
			t.Fatal(got, err)
		}
		rows, err := catalog.Select[row](t.Context(), store, "resources", persistence.Query{})
		if err != nil || len(rows) != 1 || rows[0].OwnerID != catalog.Scope {
			t.Fatal(rows, err)
		}
		private, err := persistence.Load[row](t.Context(), store, "private", "member/message")
		if err != nil || private.OwnerID != "member" {
			t.Fatal(private, err)
		}
	}
}
