package sqlitestore_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func openStore(t *testing.T) *sqlitestore.Store {
	t.Helper()
	s, err := sqlitestore.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func TestStoreCommitRollbackAndReopen(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	sentinel := errors.New("rollback")
	err := s.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "test", "a", 42); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	var value int
	if err := s.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return tx.Get(ctx, "test", "a", &value) }); core.CodeOf(err) != core.NotFound {
		t.Fatal(err)
	}
	if err := s.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return tx.Put(ctx, "test", "a", 42) }); err != nil {
		t.Fatal(err)
	}
	path := s.Path()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return tx.Get(ctx, "test", "a", &value) }); err != nil || value != 42 {
		t.Fatal(value, err)
	}
}
func TestStoreNestedSavepointAndExpiredScope(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	var bound persistence.Store
	err := s.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		bound = tx.Store()
		var optional int
		if err := s.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return tx.Get(ctx, "test", "missing", &optional) }); core.CodeOf(err) != core.NotFound {
			return errors.New("wrong missing result")
		}
		if err := s.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return tx.Put(ctx, "test", "good", 1) }); err != nil {
			return err
		}
		fail := errors.New("nested failure")
		err := bound.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			if err := tx.Put(ctx, "test", "bad", 2); err != nil {
				return err
			}
			return fail
		})
		if !errors.Is(err, fail) {
			return errors.New("nested error lost")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := bound.Read(ctx, func(context.Context, persistence.Tx) error { return nil }); core.CodeOf(err) != core.FailedPrecondition {
		t.Fatal("expired scope", err)
	}
	err = s.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var v int
		if err := tx.Get(ctx, "test", "good", &v); err != nil {
			return err
		}
		if v != 1 {
			return errors.New("bad value")
		}
		if err := tx.Get(ctx, "test", "bad", &v); core.CodeOf(err) != core.NotFound {
			return errors.New("nested write committed")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestStoreReadOnlyCanceledAndCommitFailure(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	if err := s.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return tx.Put(ctx, "test", "bad", 1) }); core.CodeOf(err) != core.FailedPrecondition {
		t.Fatal(err)
	}
	ctx2, cancel := context.WithCancel(ctx)
	cancel()
	called := false
	if err := s.Write(ctx2, func(context.Context, persistence.Tx) error { called = true; return nil }); core.CodeOf(err) != core.Canceled || called {
		t.Fatal(err, called)
	}
	injected := errors.New("commit failed")
	s.FailNextCommit(injected)
	if err := s.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return tx.Put(ctx, "test", "fail", 1) }); !errors.Is(err, injected) {
		t.Fatal(err)
	}
	if err := s.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { var x int; return tx.Get(ctx, "test", "fail", &x) }); core.CodeOf(err) != core.NotFound {
		t.Fatal(err)
	}
}
func TestStoreConcurrentIncrements(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	errs := make(chan error, 20)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				var n int
				err := tx.Get(ctx, "counter", "n", &n)
				if err != nil && !persistence.Missing(err) {
					return err
				}
				return tx.Put(ctx, "counter", "n", n+1)
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var total int
	if err := s.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return tx.Get(ctx, "counter", "n", &total) }); err != nil || total != 20 {
		t.Fatal(total, err)
	}
}
func TestStoreScanDeleteAndSnapshot(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	if err := s.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "rows", "b", 2); err != nil {
			return err
		}
		return tx.Put(ctx, "rows", "a", 1)
	}); err != nil {
		t.Fatal(err)
	}
	err := s.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		rows, err := tx.Scan(ctx, "rows")
		if err != nil {
			return err
		}
		if len(rows) != 2 || rows[0].Key != "a" {
			return errors.New("unsorted scan")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot.db")
	if err := s.Snapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := s.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return tx.Delete(ctx, "rows", "a") }); err != nil {
		t.Fatal(err)
	}
	copy, err := sqlitestore.Open(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	var value int
	if err := copy.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return tx.Get(ctx, "rows", "a", &value) }); err != nil || value != 1 {
		t.Fatal(value, err)
	}
}
