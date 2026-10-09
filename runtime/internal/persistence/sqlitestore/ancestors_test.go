package sqlitestore

import (
	"context"
	"molr.ai/runtime/internal/persistence"
	"path/filepath"
	"testing"
)

func TestAncestorsBoundedOrderedAndTransactional(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "chain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	err = s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for k, p := range map[string]string{"a": "root", "b": "a", "c": "b"} {
			if err := tx.Put(ctx, "test", k, map[string]string{"Parent": p}); err != nil {
				return err
			}
		}
		rows, err := tx.Ancestors(ctx, "test", "c", "Parent", 10)
		if err != nil {
			return err
		}
		if len(rows) != 3 || rows[0].Key != "c" || rows[2].Key != "a" {
			t.Fatal(rows)
		}
		if err := tx.Put(ctx, "test", "a", map[string]string{"Parent": "c"}); err != nil {
			return err
		}
		rows, err = tx.Ancestors(ctx, "test", "c", "Parent", 4)
		if err != nil {
			return err
		}
		if len(rows) != 4 {
			t.Fatal("cycle not bounded", len(rows))
		}
		if _, err := tx.Ancestors(ctx, "test", "c", "Parent') --", 4); err == nil {
			t.Fatal("untrusted field accepted")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
