package app

import (
	"context"
	"errors"
	"molr.ai/runtime/internal/core"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestResourceFileLockRejectsCanceledContext(t *testing.T) {
	f := &resourceFileTools{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	acquired := 0
	for range 256 {
		unlock, err := f.lock(ctx, "synthetic-path")
		if err == nil {
			acquired++
			unlock()
		} else if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	t.Logf("already-canceled requests acquired the available lock %d/256 times", acquired)
	if acquired != 0 {
		t.Errorf("accepted %d already-canceled requests", acquired)
	}
	if len(f.locks) != 0 {
		t.Errorf("retained %d unused lock entries", len(f.locks))
	}
}

// Cancel between the initial context check and selecting the available token.
type cancelOnDoneContext struct {
	context.Context
	cancel context.CancelFunc
}

func (c cancelOnDoneContext) Done() <-chan struct{} { c.cancel(); return c.Context.Done() }

func TestResourceFileLockCancellationDuringAcquisitionReleasesToken(t *testing.T) {
	f := &resourceFileTools{}
	for range 256 {
		parent, cancel := context.WithCancel(t.Context())
		ctx := cancelOnDoneContext{parent, cancel}
		unlock, err := f.lock(ctx, "shared")
		if unlock != nil {
			unlock()
			t.Fatal("canceled acquisition returned a lock")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if len(f.locks) != 0 {
			t.Fatal("canceled acquisition retained an entry")
		}
		normal, err := f.lock(t.Context(), "shared")
		if err != nil {
			t.Fatal(err)
		}
		normal()
	}
}

func TestResourceFileLockDeadlineAndWaitingCancellation(t *testing.T) {
	f := &resourceFileTools{}
	expired, cancelExpired := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancelExpired()
	if unlock, err := f.lock(expired, "shared"); !errors.Is(err, context.DeadlineExceeded) || unlock != nil {
		if unlock != nil {
			unlock()
		}
		t.Fatal("expired request", err)
	}
	held, err := f.lock(t.Context(), "shared")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		unlock, err := f.lock(ctx, "shared")
		if unlock != nil {
			unlock()
		}
		done <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		f.mu.Lock()
		refs := f.locks["shared"].refs
		f.mu.Unlock()
		if refs == 2 {
			break
		}
		if time.Now().After(deadline) {
			held()
			t.Fatal("waiter did not register")
		}
		time.Sleep(time.Millisecond)
	}
	other, err := f.lock(t.Context(), "other")
	if err != nil {
		held()
		t.Fatal(err)
	}
	other()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			held()
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		held()
		t.Fatal("canceled waiter remained blocked")
	}
	f.mu.Lock()
	refs := f.locks["shared"].refs
	f.mu.Unlock()
	if refs != 1 {
		held()
		t.Fatalf("holder reference count=%d", refs)
	}
	held()
	again, err := f.lock(t.Context(), "shared")
	if err != nil {
		t.Fatal(err)
	}
	again()
	if len(f.locks) != 0 {
		t.Fatal("completed operations retained lock entries")
	}
}

func TestResourceFileLockSerializesConcurrentWriters(t *testing.T) {
	f := &resourceFileTools{}
	var inside atomic.Int32
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			for range 20 {
				unlock, err := f.lock(t.Context(), "shared")
				if err != nil {
					t.Error(err)
					return
				}
				if inside.Add(1) != 1 {
					t.Error("concurrent critical sections")
				}
				runtime.Gosched()
				inside.Add(-1)
				unlock()
			}
		})
	}
	wg.Wait()
	if len(f.locks) != 0 {
		t.Fatal("concurrent operations retained lock entries")
	}
}

func TestCanceledMediaSnapshotDoesNotTouchOrEvictCache(t *testing.T) {
	f := &resourceFileTools{adapters: adapters{dataDir: t.TempDir()}}
	e := core.Execution{Principal: core.Principal{OwnerID: "owner"}, AssistantID: "assistant"}
	data := []byte("existing synthetic snapshot")
	hash, err := f.snapshot(t.Context(), e, data)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.mediaRoot(e), hash)
	old := time.Now().Add(-8 * 24 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, input := range [][]byte{data, []byte("new synthetic snapshot")} {
		if id, err := f.snapshot(ctx, e, input); id != "" || !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled snapshot returned %q, %v", id, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal("canceled operation evicted cached media", err)
		}
		if !info.ModTime().Equal(old) {
			t.Fatal("canceled operation touched cached media")
		}
	}
	files, err := os.ReadDir(f.mediaRoot(e))
	if err != nil || len(files) != 1 {
		t.Fatal("canceled operation created media", files, err)
	}
}
