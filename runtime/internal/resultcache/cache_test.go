package resultcache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestCache(t *testing.T, entries int, cost int64) *Cache[string, string] {
	t.Helper()
	c := New[string, string](Options[string]{MaxEntries: entries, MaxCost: cost, LoadTimeout: time.Second, Cost: func(v string) int64 { return int64(len(v)) }})
	t.Cleanup(c.Close)
	return c
}
func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !condition() {
		select {
		case <-deadline:
			t.Fatal("condition not reached")
		case <-tick.C:
		}
	}
}
func TestCacheFixedTTLRefreshAndFailure(t *testing.T) {
	c := newTestCache(t, 4, 100)
	now := time.Now()
	c.now = func() time.Time { return now }
	calls := 0
	load := func(context.Context) (string, error) { calls++; return "value", nil }
	first, err := c.GetOrCompute(t.Context(), "key", time.Minute, false, load)
	if err != nil || first.Hit || !first.Stored || !first.WrittenAt.Equal(now) {
		t.Fatal(first, err)
	}
	now = now.Add(59 * time.Second)
	hit, err := c.GetOrCompute(t.Context(), "key", time.Minute, false, load)
	if err != nil || !hit.Hit || calls != 1 || hit.WrittenAt != first.WrittenAt {
		t.Fatal(hit, err, calls)
	}
	now = now.Add(time.Second)
	expired, err := c.GetOrCompute(t.Context(), "key", time.Minute, false, load)
	if err != nil || expired.Hit || calls != 2 || expired.WrittenAt != now {
		t.Fatal(expired, err, calls)
	}
	now = now.Add(time.Second)
	fresh, err := c.GetOrCompute(t.Context(), "key", time.Minute, true, load)
	if err != nil || fresh.Hit || calls != 3 || fresh.WrittenAt != now {
		t.Fatal(fresh, err, calls)
	}
	failure := errors.New("failed")
	if _, err = c.GetOrCompute(t.Context(), "key", time.Minute, true, func(context.Context) (string, error) { return "", failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	kept, err := c.GetOrCompute(t.Context(), "key", time.Minute, false, load)
	if err != nil || !kept.Hit || kept.WrittenAt != fresh.WrittenAt {
		t.Fatal(kept, err)
	}
	for range 2 {
		_, err = c.GetOrCompute(t.Context(), "error", time.Minute, false, func(context.Context) (string, error) { return "", failure })
		if !errors.Is(err, failure) {
			t.Fatal(err)
		}
	}
	if _, ok := c.entries["error"]; ok {
		t.Fatal("error cached")
	}
	bypass, err := c.GetOrCompute(t.Context(), "disabled", 0, false, load)
	if err != nil || bypass.Stored || !bypass.WrittenAt.IsZero() {
		t.Fatal(bypass, err)
	}
}
func TestCacheLRUCostAndOversizedResults(t *testing.T) {
	c := newTestCache(t, 2, 6)
	get := func(key, value string) Result[string] {
		t.Helper()
		r, e := c.GetOrCompute(t.Context(), key, time.Minute, false, func(context.Context) (string, error) { return value, nil })
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	get("a", "aa")
	get("b", "bb")
	get("a", "unused")
	get("c", "ccc")
	if _, ok := c.entries["b"]; ok {
		t.Fatal("least recently used entry retained")
	}
	if c.cost != 5 || len(c.entries) != 2 {
		t.Fatal(c.cost, len(c.entries))
	}
	huge := get("huge", "1234567")
	if huge.Stored || huge.Hit || !huge.WrittenAt.IsZero() || len(c.entries) != 2 {
		t.Fatal(huge)
	}
	fresh, err := c.GetOrCompute(t.Context(), "a", time.Minute, true, func(context.Context) (string, error) { return "1234567", nil })
	if err != nil || fresh.Stored {
		t.Fatal(fresh, err)
	}
	if _, ok := c.entries["a"]; ok {
		t.Fatal("oversized refresh retained an obsolete value")
	}
}
func TestCacheSharesWorkAndIndependentCancellation(t *testing.T) {
	c := newTestCache(t, 4, 100)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	load := func(ctx context.Context) (string, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return "shared", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	first, cancel := context.WithCancel(t.Context())
	firstDone := make(chan error, 1)
	secondDone := make(chan Result[string], 1)
	go func() { _, err := c.GetOrCompute(first, "key", time.Minute, false, load); firstDone <- err }()
	<-started
	go func() {
		r, err := c.GetOrCompute(t.Context(), "key", time.Minute, true, load)
		if err != nil {
			t.Error(err)
		}
		secondDone <- r
	}()
	waitFor(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.flights["key"].waiters == 2 })
	cancel()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	r := <-secondDone
	if r.Value != "shared" || !r.Stored || r.Hit || calls.Load() != 1 {
		t.Fatal(r, calls.Load())
	}
}
func TestCacheAbandonedWorkCannotOverwriteReplacement(t *testing.T) {
	c := newTestCache(t, 4, 100)
	ctx, cancel := context.WithCancel(t.Context())
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := c.GetOrCompute(ctx, "key", time.Minute, false, func(context.Context) (string, error) { close(started); <-release; close(finished); return "old", nil })
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	r, err := c.GetOrCompute(t.Context(), "key", time.Minute, true, func(context.Context) (string, error) { return "new", nil })
	if err != nil || r.Value != "new" {
		t.Fatal(r, err)
	}
	close(release)
	<-finished
	// The abandoned loader's context is canceled even if it ignores cancellation.
	for range 10 {
		r, err = c.GetOrCompute(t.Context(), "key", time.Minute, false, func(context.Context) (string, error) { t.Error("unexpected recomputation"); return "", nil })
		if err != nil || r.Value != "new" {
			t.Fatal(r, err)
		}
	}
}
func TestCacheLimitsTimeoutPanicAndClose(t *testing.T) {
	c := newTestCache(t, 1, 100)
	c.options.LoadTimeout = 20 * time.Millisecond
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := c.GetOrCompute(t.Context(), "one", time.Minute, false, func(ctx context.Context) (string, error) { close(started); <-ctx.Done(); return "", ctx.Err() })
		done <- err
	}()
	<-started
	if _, err := c.GetOrCompute(t.Context(), "two", time.Minute, false, func(context.Context) (string, error) { return "", nil }); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if _, err := c.GetOrCompute(t.Context(), "panic", time.Minute, false, func(context.Context) (string, error) { panic("fixture") }); !errors.Is(err, ErrPanic) {
		t.Fatal(err)
	}
	started = make(chan struct{})
	go func() {
		_, err := c.GetOrCompute(t.Context(), "one", time.Minute, false, func(ctx context.Context) (string, error) { close(started); <-ctx.Done(); return "", ctx.Err() })
		done <- err
	}()
	<-started
	c.Close()
	c.Close()
	if err := <-done; !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if _, err := c.GetOrCompute(t.Context(), "closed", time.Minute, false, func(context.Context) (string, error) { return "", nil }); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}
func TestCacheConcurrentAccess(t *testing.T) {
	c := newTestCache(t, 4, 100)
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			for range 10 {
				_, err := c.GetOrCompute(t.Context(), "same", time.Minute, false, func(context.Context) (string, error) { return "value", nil })
				if err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	if len(c.entries) != 1 || c.cost != 5 {
		t.Fatal(c.cost)
	}
}
