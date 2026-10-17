package testkit

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/persistence"
	"sync/atomic"
	"testing"
	"time"
)

// IdleStore counts actual storage transactions without replacing persistence.
type IdleStore struct {
	persistence.Store
	Calls   atomic.Int64
	Active  atomic.Int64
	Watches atomic.Int64
}

func (s *IdleStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	s.Calls.Add(1)
	s.Active.Add(1)
	defer func() { s.Calls.Add(1); s.Active.Add(-1) }()
	return s.Store.Read(ctx, fn)
}
func (s *IdleStore) Write(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	s.Calls.Add(1)
	s.Active.Add(1)
	defer func() { s.Calls.Add(1); s.Active.Add(-1) }()
	return s.Store.Write(ctx, fn)
}
func (s *IdleStore) Watch(key persistence.SignalKey) (persistence.Watch, error) {
	s.Watches.Add(1)
	return s.Store.Watch(key)
}
func (s *IdleStore) RequireIdle(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := s.waitIdle(ctx); err != nil {
		t.Fatal(err)
	}
}

func (s *IdleStore) waitIdle(ctx context.Context) error {
	// Longer than both former 1s connection/delivery and 250ms inbox periods.
	// Every transaction resets the window, including work from a delayed wakeup.
	const quietWindow = 1200 * time.Millisecond
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	previous := s.Calls.Load()
	var quietSince time.Time
	for {
		calls := s.Calls.Load()
		if calls != previous || s.Active.Load() != 0 || s.Watches.Load() == 0 {
			quietSince = time.Time{}
		} else if quietSince.IsZero() {
			quietSince = time.Now()
		} else if time.Since(quietSince) >= quietWindow {
			return nil
		}
		previous = calls
		select {
		case <-ctx.Done():
			return fmt.Errorf("channel did not sustain %s without storage activity (calls=%d active=%d watches=%d): %w", quietWindow, s.Calls.Load(), s.Active.Load(), s.Watches.Load(), ctx.Err())
		case <-ticker.C:
		}
	}
}
