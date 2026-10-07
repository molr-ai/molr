package sqlite_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/storage/sqlite"
	"molr.ai/runtime/tests/testkit"
)

func TestTDDSQLiteInstanceLockExcludesAnotherOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	first, err := sqlite.Open(path)
	testkit.RequireNoError(t, err)
	t.Cleanup(func() { _ = first.Close() })
	_, err = sqlite.Open(path)
	testkit.RequireCode(t, err, core.Conflict)
	testkit.RequireNoError(t, first.Close())
	second, err := sqlite.Open(path)
	testkit.RequireNoError(t, err)
	t.Cleanup(func() { _ = second.Close() })
	testkit.RequireNoError(t, second.Migrate(testkit.Context(t)))
}
func TestTDDSQLiteCorruptBackupLeavesOriginalUntouched(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "runtime.db")
	ctx := testkit.Context(t)
	db, err := sqlite.Open(path)
	testkit.RequireNoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	testkit.RequireNoError(t, db.Migrate(ctx))
	original := receipt("original-data")
	testkit.RequireNoError(t, db.Within(ctx, func(r sqlite.Repositories) error { return r.Idempotency().Insert(ctx, original) }))
	backup, err := db.Backup(ctx)
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, db.Close())
	snapshot := filepath.Join(directory, "backups", "runtime.db", string(backup.ID)+".db")
	testkit.RequireNoError(t, os.WriteFile(snapshot, []byte("tampered SQLite snapshot"), 0600))
	restorer, err := sqlite.NewRestorer(path)
	testkit.RequireNoError(t, err)
	testkit.RequireCode(t, restorer.Restore(ctx, backup), core.FailedPrecondition)
	reopened, err := sqlite.Open(path)
	testkit.RequireNoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	got, err := stored(t, reopened, ctx, original.Key)
	testkit.RequireNoError(t, err)
	if got.InputDigest != original.InputDigest {
		t.Fatal("failed restore changed original data")
	}
}
func TestTDDSQLiteRestoreRejectsSymlinkSnapshot(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "runtime.db")
	ctx := testkit.Context(t)
	db, err := sqlite.Open(path)
	testkit.RequireNoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	testkit.RequireNoError(t, db.Migrate(ctx))
	backup, err := db.Backup(ctx)
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, db.Close())
	snapshot := filepath.Join(directory, "backups", "runtime.db", string(backup.ID)+".db")
	moved := snapshot + ".original"
	testkit.RequireNoError(t, os.Rename(snapshot, moved))
	testkit.RequireNoError(t, os.Symlink(moved, snapshot))
	restorer, err := sqlite.NewRestorer(path)
	testkit.RequireNoError(t, err)
	testkit.RequireCode(t, restorer.Restore(ctx, backup), core.Forbidden)
}
func TestTDDSQLiteCanceledRestoreDoesNotAcquireOrChangeDatabase(t *testing.T) {
	f, ctx := initialized(t)
	backup, err := f.DB.Backup(ctx)
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, f.DB.Close())
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	testkit.RequireCode(t, f.Restorer.Restore(canceled, backup), core.Canceled)
	reopened := f.Reopen()
	t.Cleanup(func() { _ = reopened.Close() })
	testkit.RequireNoError(t, reopened.Check(ctx))
}
