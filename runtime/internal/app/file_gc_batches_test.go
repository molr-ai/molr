package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/storage/files"
)

type gcWriteCounter struct {
	persistence.Store
	writes int
}

func (s *gcWriteCounter) Write(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	s.writes++
	return s.Store.Write(ctx, fn)
}

type gcRemovalFixture struct {
	files.Objects
	removed int
	cancel  context.CancelFunc
}

func (o *gcRemovalFixture) Remove(ctx context.Context, _ core.ID) error {
	o.removed++
	if o.cancel != nil {
		o.cancel()
	}
	return ctx.Err()
}
func seedGCQueue(ctx context.Context, store persistence.Store, count int) error {
	return store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < count; i++ {
			id := core.ID(fmt.Sprintf("file-%04d", i))
			if err := tx.Put(ctx, "files.gc", persistence.Key("owner", id), id); err != nil {
				return err
			}
		}
		return nil
	})
}
func TestFileGCCommitsCompletedRemovalsInBatches(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "gc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := seedGCQueue(t.Context(), db, 100); err != nil {
		t.Fatal(err)
	}
	store := &gcWriteCounter{Store: db}
	objects := &gcRemovalFixture{}
	if err := collectFiles(t.Context(), store, objects); err != nil {
		t.Fatal(err)
	}
	if objects.removed != 100 {
		t.Fatalf("removed %d files", objects.removed)
	}
	pending, err := persistence.Scan[core.ID](t.Context(), db, "files.gc")
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending=%d err=%v", len(pending), err)
	}
	if store.writes > 2 {
		t.Fatalf("100 removed files caused %d separate write transactions", store.writes)
	}
}
func TestFileGCStopsOnCanceledRemoval(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "gc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := seedGCQueue(t.Context(), db, 100); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	objects := &gcRemovalFixture{cancel: cancel}
	err = collectFiles(ctx, db, objects)
	if core.CodeOf(err) != core.Canceled || objects.removed != 1 {
		t.Fatalf("canceled GC attempted %d removals and returned %v", objects.removed, err)
	}
}
func BenchmarkFileGCCompletedRemovals(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprintf("files-%d", count), func(b *testing.B) {
			db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "gc.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			store := &gcWriteCounter{Store: db}
			objects := &gcRemovalFixture{}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				if err := seedGCQueue(b.Context(), db, count); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				if err := collectFiles(b.Context(), store, objects); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(store.writes)/float64(b.N), "transactions/op")
		})
	}
}

func gcDiskFixture(t *testing.T, count int) (string, *sqlitestore.Store, *files.ObjectStore) {
	t.Helper()
	root := t.TempDir()
	db, err := sqlitestore.Open(filepath.Join(root, "gc.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	objects, err := files.NewObjects(filepath.Join(root, "files"), db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { objects.Close() })
	for i := 0; i < count; i++ {
		id := core.ID(fmt.Sprintf("file-%04d", i))
		staged, err := objects.Put(t.Context(), id, strings.NewReader("discard"), 7)
		if err != nil {
			t.Fatal(err)
		}
		if err := objects.Publish(t.Context(), staged, func(commit func() error) error { return commit() }); err != nil {
			t.Fatal(err)
		}
	}
	if err := seedGCQueue(t.Context(), db, count); err != nil {
		t.Fatal(err)
	}
	return root, db, objects
}
func pendingGC(t *testing.T, store persistence.Store) []core.ID {
	t.Helper()
	rows, err := persistence.Scan[core.ID](t.Context(), store, "files.gc")
	if err != nil {
		t.Fatal(err)
	}
	return rows
}
func TestFileGCCommitFailureSurvivesRestart(t *testing.T) {
	root, db, objects := gcDiskFixture(t, 5)
	failure := errors.New("queue commit interrupted")
	db.FailNextCommit(failure)
	if err := collectFiles(t.Context(), db, objects); !errors.Is(err, failure) {
		t.Fatalf("lost commit error: %v", err)
	}
	if len(pendingGC(t, db)) != 5 {
		t.Fatal("failed commit acknowledged removed files")
	}
	for i := 0; i < 5; i++ {
		if body, err := objects.Open(t.Context(), core.ID(fmt.Sprintf("file-%04d", i))); err == nil {
			body.Close()
			t.Fatal("object was not removed before acknowledgement")
		}
	}
	objects.Close()
	db.Close()
	reopened, err := sqlitestore.Open(filepath.Join(root, "gc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recovered, err := files.NewObjects(filepath.Join(root, "files"), reopened)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if err := recovered.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(pendingGC(t, reopened)) != 5 {
		t.Fatal("restart lost pending cleanup")
	}
	if err := collectFiles(t.Context(), reopened, recovered); err != nil {
		t.Fatal(err)
	}
	if len(pendingGC(t, reopened)) != 0 {
		t.Fatal("idempotent cleanup retry retained queue entries")
	}
}

type gcSelectiveRemoval struct {
	files.Objects
	failed map[core.ID]bool
	cancel context.CancelFunc
	calls  int
}

func (o *gcSelectiveRemoval) Remove(ctx context.Context, id core.ID) error {
	o.calls++
	if o.failed[id] {
		return errors.New("fixture file temporarily busy")
	}
	if err := o.Objects.Remove(ctx, id); err != nil {
		return err
	}
	if o.cancel != nil && o.calls == 3 {
		o.cancel()
	}
	return nil
}
func TestFileGCRetainsFailuresButAttemptsLaterFiles(t *testing.T) {
	_, db, objects := gcDiskFixture(t, 6)
	removal := &gcSelectiveRemoval{Objects: objects, failed: map[core.ID]bool{"file-0000": true, "file-0002": true}}
	if err := collectFiles(t.Context(), db, removal); err != nil {
		t.Fatal(err)
	}
	pending := pendingGC(t, db)
	if len(pending) != 2 || pending[0] != "file-0000" || pending[1] != "file-0002" || removal.calls != 6 {
		t.Fatalf("partial cleanup: pending=%v calls=%d", pending, removal.calls)
	}
	for _, id := range pending {
		body, err := objects.Open(t.Context(), id)
		if err != nil {
			t.Fatal("failed removal lost bytes", err)
		}
		body.Close()
	}
	removal.failed = nil
	if err := collectFiles(t.Context(), db, removal); err != nil {
		t.Fatal(err)
	}
	if len(pendingGC(t, db)) != 0 {
		t.Fatal("recovered removal never retried")
	}
}
func TestFileGCCancellationLeavesPartialBatchRetryable(t *testing.T) {
	_, db, objects := gcDiskFixture(t, 6)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	removal := &gcSelectiveRemoval{Objects: objects, cancel: cancel}
	if err := collectFiles(ctx, db, removal); core.CodeOf(err) != core.Canceled {
		t.Fatalf("lost cancellation: %v", err)
	}
	if removal.calls != 3 || len(pendingGC(t, db)) != 6 {
		t.Fatal("interrupted batch was acknowledged or continued")
	}
	removal.cancel = nil
	if err := collectFiles(t.Context(), db, removal); err != nil {
		t.Fatal(err)
	}
	if len(pendingGC(t, db)) != 0 {
		t.Fatal("partial batch did not recover")
	}
}
func TestFileGCBatchBoundaries(t *testing.T) {
	for _, count := range []int{0, 1, 128, 129, 257} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "gc.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err := seedGCQueue(t.Context(), db, count); err != nil {
				t.Fatal(err)
			}
			counted := &gcWriteCounter{Store: db}
			removed := &gcRemovalFixture{}
			if err := collectFiles(t.Context(), counted, removed); err != nil {
				t.Fatal(err)
			}
			if removed.removed != count || len(pendingGC(t, db)) != 0 || counted.writes != (count+127)/128 {
				t.Fatalf("count=%d removed=%d transactions=%d", count, removed.removed, counted.writes)
			}
		})
	}
}
