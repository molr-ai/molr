package testkit

import (
	"context"
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
	deadline := time.Now().Add(3 * time.Second)
	for {
		before := s.Calls.Load()
		time.Sleep(250 * time.Millisecond)
		if before == s.Calls.Load() && s.Watches.Load() > 0 && s.Active.Load() == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("channel never became idle")
		}
	}
	before := s.Calls.Load()
	// Longer than both former 1s connection/delivery and 250ms inbox periods.
	time.Sleep(1200 * time.Millisecond)
	if after := s.Calls.Load(); after != before {
		t.Fatalf("idle channel accessed storage: %d -> %d", before, after)
	}
}
