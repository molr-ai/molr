package testkit

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"molr.ai/runtime/internal/persistence"
)

// No database IO is needed to test the observer's timing under a virtual clock.
type idleTestStore struct{ persistence.Store }

func (idleTestStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return fn(ctx, nil)
}

func (idleTestStore) Write(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return fn(ctx, nil)
}

func (idleTestStore) Watch(persistence.SignalKey) (persistence.Watch, error) {
	return idleTestWatch{}, nil
}

type idleTestWatch struct{}

func (idleTestWatch) Changes() <-chan struct{} { return nil }
func (idleTestWatch) Done() <-chan struct{}    { return nil }
func (idleTestWatch) Close()                   {}

func newIdleTestStore(t *testing.T) *IdleStore {
	t.Helper()
	s := &IdleStore{Store: idleTestStore{}}
	w, err := s.Watch(persistence.SignalKey{Namespace: "test", Key: "binding"})
	RequireNoError(t, err)
	t.Cleanup(w.Close)
	return s
}

func TestIdleStoreWaitsForDelayedStartupWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newIdleTestStore(t)
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		started := time.Now()
		go func() {
			// Both notifications arrive after the old 250ms settling check.
			time.Sleep(500 * time.Millisecond)
			if err := s.Read(ctx, func(context.Context, persistence.Tx) error { return nil }); err != nil {
				t.Error(err)
			}
			time.Sleep(400 * time.Millisecond)
			if err := s.Write(ctx, func(context.Context, persistence.Tx) error { return nil }); err != nil {
				t.Error(err)
			}
		}()
		RequireNoError(t, s.waitIdle(ctx))
		if elapsed := time.Since(started); elapsed < 2100*time.Millisecond {
			t.Fatalf("quiet window did not restart after the last notification: %s", elapsed)
		}
	})
}

func TestIdleStoreDoesNotConfuseBlockedTransactionWithIdle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newIdleTestStore(t)
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		release := make(chan struct{})
		go func() {
			if err := s.Read(ctx, func(context.Context, persistence.Tx) error { <-release; return nil }); err != nil {
				t.Error(err)
			}
		}()
		synctest.Wait()
		done := make(chan error, 1)
		go func() { done <- s.waitIdle(ctx) }()
		time.Sleep(1500 * time.Millisecond)
		select {
		case err := <-done:
			t.Fatalf("active transaction reported idle: %v", err)
		default:
		}
		finished := time.Now()
		close(release)
		RequireNoError(t, <-done)
		if elapsed := time.Since(finished); elapsed < 1200*time.Millisecond {
			t.Fatalf("transaction completion did not restart quiet window: %s", elapsed)
		}
	})
}

func TestIdleStoreRejectsContinuousPolling(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newIdleTestStore(t)
		ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
		defer cancel()
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := s.Read(ctx, func(context.Context, persistence.Tx) error { return nil }); err != nil {
						t.Error(err)
					}
				}
			}
		}()
		if err := s.waitIdle(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("one-second polling was not rejected: %v", err)
		}
	})
}

func TestIdleStoreRequiresSubscription(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &IdleStore{Store: idleTestStore{}}
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		if err := s.waitIdle(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("worker without a subscription reported idle: %v", err)
		}
	})
}
