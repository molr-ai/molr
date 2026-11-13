package sqlitestore

import (
	"context"
	"molr.ai/runtime/internal/persistence"
	"path/filepath"
	"testing"
)

func TestUninitializedStoreDefersDerivedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, err := OpenUninitialized(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	empty, err := s.Empty(t.Context())
	if err != nil || !empty {
		t.Fatal(empty, err)
	}
	var n int
	if err = s.db.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE name LIKE 'records_query_%'").Scan(&n); err != nil || n != 0 {
		t.Fatal("query indexes initialized before version check", n, err)
	}
	if _, err = s.db.Exec(`INSERT INTO records VALUES('im.messages','owner/message','{"CreatedAt":"2026-01-01T00:00:00Z"}')`); err != nil {
		t.Fatal(err)
	}
	empty, err = s.Empty(t.Context())
	if err != nil || empty {
		t.Fatal(empty, err)
	}
	if err = s.Initialize(); err != nil {
		t.Fatal(err)
	}
	if err = s.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		var row struct{ CreatedAtKey string }
		if err := tx.Get(ctx, "im.messages", "owner/message", &row); err != nil {
			return err
		}
		if row.CreatedAtKey == "" {
			t.Fatal("missing query projection")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.Initialize(); err != nil {
		t.Fatal("repeat initialization", err)
	}
}
