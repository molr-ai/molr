package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/storage/sqlite"
	"molr.ai/runtime/tests/testkit"
)

func init() {
	newContractFixture = func(t *testing.T) contractFixture {
		path := filepath.Join(t.TempDir(), "runtime.db")
		db, err := sqlite.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := db.Close(); err != nil {
				t.Error(err)
			}
		})
		restorer, err := sqlite.NewRestorer(path)
		if err != nil {
			t.Fatal(err)
		}
		return contractFixture{DB: db, Restorer: restorer, Reopen: func() sqlite.Database {
			reopened, err := sqlite.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := reopened.Close(); err != nil {
					t.Error(err)
				}
			})
			return reopened
		}, FailNextCommit: db.FailNextCommit, FailNextMigration: db.InjectMigrationError, SetFutureSchema: func() {
			if err := db.Store().Write(testkit.Context(t), func(ctx context.Context, tx persistence.Tx) error {
				return tx.Put(ctx, "system", "schema", sqlite.Schema{Version: 999, CheckpointVersion: 999})
			}); err != nil {
				t.Fatal(err)
			}
		}}
	}
}
