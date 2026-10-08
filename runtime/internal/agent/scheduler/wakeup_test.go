package scheduler

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

type observedStore struct {
	persistence.Store
	queries    atomic.Int64
	scans      atomic.Int64
	maxRows    atomic.Int64
	afterWrite func()
	hookUsed   atomic.Bool
}
type observedTx struct {
	persistence.Tx
	store *observedStore
}

func (s *observedStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, observedTx{tx, s}) })
}
func (s *observedStore) Write(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	err := s.Store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, observedTx{tx, s}) })
	if s.afterWrite != nil && s.hookUsed.CompareAndSwap(false, true) {
		s.afterWrite()
	}
	return err
}
func (t observedTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	rows, err := t.Tx.Query(ctx, bucket, q)
	if bucket == jobsBucket {
		t.store.queries.Add(1)
		for {
			old := t.store.maxRows.Load()
			if int64(len(rows)) <= old || t.store.maxRows.CompareAndSwap(old, int64(len(rows))) {
				break
			}
		}
	}
	return rows, err
}
func (t observedTx) Scan(ctx context.Context, bucket string) ([]persistence.Record, error) {
	t.store.scans.Add(1)
	return t.Tx.Scan(ctx, bucket)
}
func openObserved(t *testing.T) (*sqlitestore.Store, *observedStore) {
	t.Helper()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, &observedStore{Store: db}
}
func startService(t *testing.T, s persistence.Store, opts Options) {
	t.Helper()
	svc := NewService(s, opts)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("scheduler did not stop")
		}
	})
}
func receiveClaim(t *testing.T, ch <-chan Claim) Claim {
	t.Helper()
	select {
	case c := <-ch:
		return c
	case <-time.After(3 * time.Second):
		t.Fatal("scheduler did not wake")
		return Claim{}
	}
}
func enqueue(t *testing.T, s persistence.Store, id, assistant string, at time.Time) {
	t.Helper()
	if err := event.NewWriter(s).AppendAssistant(t.Context(), event.AssistantEvent{ID: core.ID(id), OwnerID: "owner", AssistantID: core.ID(assistant), Kind: event.ResumeRequested, CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
}
func TestIdleWorkersOnlyReadAfterCommittedWork(t *testing.T) {
	db, s := openObserved(t)
	handled := make(chan Claim, 5)
	startService(t, s, Options{Concurrency: 1, Handle: func(_ context.Context, c Claim) error { handled <- c; return nil }})
	deadline := time.Now().Add(time.Second)
	for s.queries.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.queries.Load() < 3 {
		t.Fatal("missing startup query")
	}
	initial := s.queries.Load()
	time.Sleep(250 * time.Millisecond)
	if s.queries.Load() != initial {
		t.Fatal("idle queue was polled")
	}
	failure := errors.New("rollback")
	err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := event.NewWriter(tx.Store()).AppendAssistant(ctx, event.AssistantEvent{ID: "rolled-back", OwnerID: "owner", AssistantID: "assistant", Kind: event.ResumeRequested, CreatedAt: time.Now()}); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	db.FailNextCommit(failure)
	if err := event.NewWriter(db).AppendAssistant(t.Context(), event.AssistantEvent{ID: "failed-commit", OwnerID: "owner", AssistantID: "assistant", Kind: event.ResumeRequested, CreatedAt: time.Now()}); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if s.queries.Load() != initial {
		t.Fatal("rollback woke worker")
	}
	enqueue(t, db, "committed", "assistant", time.Now())
	if c := receiveClaim(t, handled); c.Job.Event.ID != "committed" {
		t.Fatal(c)
	}
}
func TestDeferredWorkWakesAtDeadlineAndReleasesNextInput(t *testing.T) {
	db, _ := openObserved(t)
	handled := make(chan Claim, 8)
	retryAt := time.Now().Add(250 * time.Millisecond)
	enqueue(t, db, "first", "assistant", time.Now().Add(-time.Second))
	enqueue(t, db, "second", "assistant", time.Now())
	startService(t, db, Options{Concurrency: 2, Handle: func(_ context.Context, c Claim) error {
		handled <- c
		if c.Job.Event.ID == "first" && c.Job.Attempts == 1 {
			return &Deferred{At: retryAt, Cause: core.NewError(core.Unavailable, "retry")}
		}
		return nil
	}})
	if c := receiveClaim(t, handled); c.Job.Event.ID != "first" || c.Job.Attempts != 1 {
		t.Fatal(c)
	}
	c := receiveClaim(t, handled)
	if c.Job.Event.ID != "first" || c.Job.Attempts != 2 || time.Now().Before(retryAt) {
		t.Fatal("retry order/deadline", c)
	}
	if c := receiveClaim(t, handled); c.Job.Event.ID != "second" {
		t.Fatal("completion did not release queued input", c)
	}
}
func TestClaimPagesPastBlockedAssistantsWithoutHistoryScan(t *testing.T) {
	db, s := openObserved(t)
	now := time.Now().UTC()
	err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < 10000; i++ {
			e := event.AssistantEvent{ID: core.ID(fmt.Sprintf("history-%06d", i)), OwnerID: "owner", AssistantID: "old", CreatedAt: now}
			if err := event.SaveJob(ctx, tx, Job{Event: e, State: Done, NotBefore: now, Version: 1}); err != nil {
				return err
			}
		}
		for i := 0; i < 250; i++ {
			e := event.AssistantEvent{ID: core.ID(fmt.Sprintf("blocked-%03d", i)), OwnerID: "owner", AssistantID: "busy", CreatedAt: now}
			if err := event.SaveJob(ctx, tx, Job{Event: e, State: Queued, NotBefore: now, Version: 1}); err != nil {
				return err
			}
		}
		return event.SaveJob(ctx, tx, Job{Event: event.AssistantEvent{ID: "running", OwnerID: "owner", AssistantID: "busy", CreatedAt: now}, State: Running, NotBefore: now, Version: 1})
	})
	if err != nil {
		t.Fatal(err)
	}
	enqueue(t, db, "last", "free", now.Add(time.Second))
	c, err := NewQueue(s).Claim(t.Context(), now.Add(time.Second))
	if err != nil || c.Job.Event.ID != "last" {
		t.Fatal(c, err)
	}
	if s.scans.Load() != 0 {
		t.Fatal("claim scanned retained history")
	}
	if s.maxRows.Load() > 100 {
		t.Fatal("unbounded candidate page", s.maxRows.Load())
	}
}
func TestDreamDeadlineWakesWithoutAnotherWrite(t *testing.T) {
	db, _ := openObserved(t)
	now := time.Now().UTC()
	deadline := now.Add(180 * time.Millisecond)
	if err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.dream_sessions_index", "owner/session", map[string]any{"ID": "session", "OwnerID": "owner", "AssistantID": "assistant", "State": "active", "Started": true, "Deadline": deadline})
	}); err != nil {
		t.Fatal(err)
	}
	enqueue(t, db, "message", "assistant", now)
	handled := make(chan Claim, 1)
	startService(t, db, Options{Concurrency: 1, Handle: func(_ context.Context, c Claim) error { handled <- c; return nil }})
	c := receiveClaim(t, handled)
	if time.Now().Before(deadline) || c.Job.Event.ID != "message" {
		t.Fatal(c)
	}
}
func BenchmarkIdleClaimHistory(b *testing.B) {
	for _, n := range []int{1000, 100000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "queue.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			now := time.Now().UTC()
			err = db.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
				for i := 0; i < n; i++ {
					e := event.AssistantEvent{ID: core.ID(fmt.Sprintf("job-%06d", i)), OwnerID: "owner", AssistantID: "assistant", CreatedAt: now}
					if err := event.SaveJob(ctx, tx, Job{Event: e, State: Done, NotBefore: now, Version: 1}); err != nil {
						return err
					}
					if err := tx.Put(ctx, "agent.events", persistence.Key(e.OwnerID, e.ID), e); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				b.Fatal(err)
			}
			q := NewQueue(db)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := q.Claim(b.Context(), now); core.CodeOf(err) != core.NotFound {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestCommitBetweenEmptyQueryAndWaitIsNotLost(t *testing.T) {
	db, s := openObserved(t)
	s.afterWrite = func() { enqueue(t, db, "racing-commit", "assistant", time.Now()) }
	handled := make(chan Claim, 1)
	startService(t, s, Options{Concurrency: 1, Handle: func(_ context.Context, c Claim) error { handled <- c; return nil }})
	if c := receiveClaim(t, handled); c.Job.Event.ID != "racing-commit" {
		t.Fatal(c)
	}
}

func TestClosingStoreWakesIdleScheduler(t *testing.T) {
	db, s := openObserved(t)
	svc := NewService(s, Options{Concurrency: 1, Handle: func(context.Context, Claim) error { return nil }})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()
	deadline := time.Now().Add(time.Second)
	for s.queries.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if core.CodeOf(err) != core.Unavailable {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("closed store left waiting scheduler")
	}
}
