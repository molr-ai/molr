package quota

import (
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"path/filepath"
	"testing"
)

func openLedgerStore(t *testing.T, root string) persistence.Store {
	t.Helper()
	s, err := sqlitestore.Open(filepath.Join(root, "quota.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
