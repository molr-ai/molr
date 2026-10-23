package persistence_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

type versionedRecord struct {
	Version core.Version
	Value   string
}

func writeStore(t *testing.T) *sqlitestore.Store {
	t.Helper()
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "writes.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	return store
}

func TestVersionedWritesPreserveConflictsAndRollback(t *testing.T) {
	store, ctx := writeStore(t), t.Context()
	version := func(v versionedRecord) core.Version { return v.Version }
	key := persistence.Key("owner", "record")
	if err := persistence.Insert(ctx, store, "records", key, versionedRecord{1, "original"}); err != nil {
		t.Fatal(err)
	}
	if err := persistence.Insert(ctx, store, "records", key, versionedRecord{2, "duplicate"}); core.CodeOf(err) != core.Conflict {
		t.Fatal(err)
	}
	for _, tc := range []struct{ expected, next core.Version }{{0, 1}, {2, 3}, {1, 1}, {1, 3}} {
		if err := persistence.SaveVersion(ctx, store, "records", key, versionedRecord{tc.next, "invalid"}, tc.expected, version); core.CodeOf(err) != core.Conflict {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	if err := persistence.SaveVersion(ctx, store, "records", "missing", versionedRecord{2, "missing"}, 1, version); !persistence.Missing(err) {
		t.Fatal(err)
	}
	if err := persistence.DeleteVersion(ctx, store, "records", "missing", 1, version); !persistence.Missing(err) {
		t.Fatal(err)
	}
	if err := persistence.SaveVersion(ctx, store, "records", key, versionedRecord{2, "saved"}, 1, version); err != nil {
		t.Fatal(err)
	}

	failure := errors.New("rollback business operation")
	err := store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := persistence.SaveVersion(ctx, tx.Store(), "records", key, versionedRecord{3, "rolled back"}, 2, version); err != nil {
			return err
		}
		if err := persistence.Insert(ctx, tx.Store(), "records", "related", versionedRecord{1, "rolled back"}); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	current, err := persistence.Load[versionedRecord](ctx, store, "records", key)
	if err != nil || current != (versionedRecord{2, "saved"}) {
		t.Fatal(current, err)
	}
	if _, err := persistence.Load[versionedRecord](ctx, store, "records", "related"); !persistence.Missing(err) {
		t.Fatal(err)
	}

	// A rejected nested operation must not poison a successful outer transaction.
	err = store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := persistence.DeleteVersion(ctx, tx.Store(), "records", key, 1, version); core.CodeOf(err) != core.Conflict {
			return errors.New("stale delete accepted")
		}
		return persistence.Insert(ctx, tx.Store(), "records", persistence.Key("other", "record"), versionedRecord{1, "other owner"})
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err := persistence.Load[versionedRecord](ctx, store, "records", persistence.Key("other", "record"))
	if err != nil || other.Value != "other owner" {
		t.Fatal(other, err)
	}
	if err := persistence.DeleteVersion(ctx, store, "records", key, 0, version); core.CodeOf(err) != core.Conflict {
		t.Fatal(err)
	}
	store.FailNextCommit(failure)
	if err := persistence.DeleteVersion(ctx, store, "records", key, 2, version); err == nil {
		t.Fatal("failed commit accepted")
	}
	if _, err := persistence.Load[versionedRecord](ctx, store, "records", key); err != nil {
		t.Fatal("failed delete lost record", err)
	}
	if err := persistence.DeleteVersion(ctx, store, "records", key, 2, version); err != nil {
		t.Fatal(err)
	}
	if _, err := persistence.Load[versionedRecord](ctx, store, "records", key); !persistence.Missing(err) {
		t.Fatal(err)
	}
}

func TestVersionedWritesSerializeConcurrentUpdates(t *testing.T) {
	store, ctx := writeStore(t), t.Context()
	if err := persistence.Insert(ctx, store, "records", "one", versionedRecord{1, "before"}); err != nil {
		t.Fatal(err)
	}
	start, results := make(chan struct{}), make(chan error, 2)
	for _, value := range []string{"first", "second"} {
		go func() {
			<-start
			results <- persistence.SaveVersion(ctx, store, "records", "one", versionedRecord{2, value}, 1, func(v versionedRecord) core.Version { return v.Version })
		}()
	}
	close(start)
	var saved, conflicted int
	for range 2 {
		err := <-results
		if err == nil {
			saved++
		} else if core.CodeOf(err) == core.Conflict {
			conflicted++
		} else {
			t.Fatal(err)
		}
	}
	if saved != 1 || conflicted != 1 {
		t.Fatalf("saved=%d conflicted=%d", saved, conflicted)
	}
	current, err := persistence.Load[versionedRecord](ctx, store, "records", "one")
	if err != nil || current.Version != 2 || current.Value == "before" {
		t.Fatal(current, err)
	}
}
