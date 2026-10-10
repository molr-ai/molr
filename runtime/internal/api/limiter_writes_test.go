package api

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

type limiterWriteCounter struct {
	persistence.Store
	puts int
}
type limiterCountingTx struct {
	persistence.Tx
	counter *limiterWriteCounter
}

func (s *limiterWriteCounter) Write(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, limiterCountingTx{tx, s}) })
}
func (tx limiterCountingTx) Put(ctx context.Context, bucket, key string, value any) error {
	if bucket == "api.limits" {
		tx.counter.puts++
	}
	return tx.Tx.Put(ctx, bucket, key, value)
}

func TestDeniedRateLimitDoesNotRewritePersistedCounter(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := &limiterWriteCounter{Store: db}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	limiter, err := NewRateLimiter(store, RateLimitOptions{Now: func() time.Time { return now }, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key := RateKey{Category: "read", Address: "192.0.2.1"}
	allowed, err := limiter.Allow(ctx, key)
	if err != nil || !allowed.Allowed {
		t.Fatalf("initial allowance: %+v, %v", allowed, err)
	}
	if store.puts != 1 {
		t.Fatalf("initial counter writes = %d, want 1", store.puts)
	}
	for i := 0; i < 100; i++ {
		denied, err := limiter.Allow(ctx, key)
		if err != nil || denied.Allowed || denied.RetryAfter != time.Minute-time.Duration(i)*100*time.Millisecond {
			t.Fatalf("denial %d: %+v, %v", i, denied, err)
		}
		now = now.Add(100 * time.Millisecond)
	}
	if store.puts != 1 {
		t.Fatalf("100 rejected requests rewrote the unchanged counter %d times", store.puts-1)
	}
	now = now.Add(50 * time.Second)
	allowed, err = limiter.Allow(ctx, key)
	if err != nil || !allowed.Allowed {
		t.Fatalf("next window: %+v, %v", allowed, err)
	}
	if store.puts != 2 {
		t.Fatalf("window recovery writes = %d, want 2", store.puts)
	}
}

func BenchmarkDeniedRateLimit(b *testing.B) {
	db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "runtime.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	store := &limiterWriteCounter{Store: db}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	limiter, err := NewRateLimiter(store, RateLimitOptions{Now: func() time.Time { return now }, Limit: 1})
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	key := RateKey{Category: "read", Address: "192.0.2.1"}
	if a, err := limiter.Allow(ctx, key); err != nil || !a.Allowed {
		b.Fatalf("initial allowance: %+v, %v", a, err)
	}
	store.puts = 0
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if a, err := limiter.Allow(ctx, key); err != nil || a.Allowed {
			b.Fatalf("denial: %+v, %v", a, err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(store.puts)/float64(b.N), "writes/op")
}

func TestRateLimitConcurrentAdmissionAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	db, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	options := RateLimitOptions{Now: func() time.Time { return now }, Limit: 1, CategoryLimits: map[string]int{"read": 7}}
	limiter, err := NewRateLimiter(db, options)
	if err != nil {
		t.Fatal(err)
	}
	key := RateKey{Category: "read", Address: "192.0.2.1"}
	ctx := context.Background()
	var admitted atomic.Int32
	var group sync.WaitGroup
	for i := 0; i < 32; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			a, err := limiter.Allow(ctx, key)
			if err != nil {
				t.Error(err)
				return
			}
			if a.Allowed {
				admitted.Add(1)
			} else if a.RetryAfter != time.Minute {
				t.Errorf("retry delay = %v", a.RetryAfter)
			}
		}()
	}
	group.Wait()
	if admitted.Load() != 7 {
		t.Fatalf("concurrent admissions = %d, want category limit 7", admitted.Load())
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	limiter, err = NewRateLimiter(db, options)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(59 * time.Second)
	a, err := limiter.Allow(ctx, key)
	if err != nil || a.Allowed || a.RetryAfter != time.Second {
		t.Fatalf("persisted denial: %+v, %v", a, err)
	}
	for _, other := range []RateKey{{Category: "read", Address: "192.0.2.2"}, {Category: "write", Address: "192.0.2.1"}} {
		a, err := limiter.Allow(ctx, other)
		if err != nil || !a.Allowed {
			t.Fatalf("independent key: %+v, %v", a, err)
		}
	}
	now = now.Add(time.Second)
	a, err = limiter.Allow(ctx, key)
	if err != nil || !a.Allowed {
		t.Fatalf("window boundary: %+v, %v", a, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	a, err = limiter.Allow(ctx, key)
	if err == nil || a.Allowed {
		t.Fatalf("closed store must not allow: %+v, %v", a, err)
	}
}
