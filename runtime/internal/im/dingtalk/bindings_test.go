package dingtalk

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type bindingStore struct {
	persistence.Store
	scans    atomic.Int32
	reads    atomic.Int32
	snapshot func()
	once     sync.Once
}
type bindingTx struct {
	persistence.Tx
	store *bindingStore
}

func (s *bindingStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	before := s.scans.Load()
	err := s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, bindingTx{tx, s}) })
	if s.snapshot != nil && s.scans.Load() > before {
		s.once.Do(s.snapshot)
	}
	return err
}
func (tx bindingTx) Scan(ctx context.Context, bucket string) ([]persistence.Record, error) {
	if bucket == bindingsBucket {
		tx.store.scans.Add(1)
	}
	return tx.Tx.Scan(ctx, bucket)
}
func (tx bindingTx) Get(ctx context.Context, bucket, key string, out any) error {
	if bucket == bindingsBucket {
		tx.store.reads.Add(1)
	}
	return tx.Tx.Get(ctx, bucket, key, out)
}

type observedClient struct {
	*fakeClient
	started, stopped chan string
}

func (c *observedClient) Stream(ctx context.Context, credentials Credentials, ready func() error, _ func(context.Context, Incoming) error) error {
	if err := ready(); err != nil {
		return err
	}
	c.started <- credentials.ClientID
	<-ctx.Done()
	c.stopped <- credentials.ClientID
	return ctx.Err()
}
func awaitStream(t *testing.T, c <-chan string) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(3 * time.Second):
		t.Fatal("stream transition missing")
	}
}
func runObserved(t *testing.T, f *fixture) (*observedClient, *bindingStore) {
	t.Helper()
	c := &observedClient{fakeClient: f.client, started: make(chan string, 16), stopped: make(chan string, 16)}
	f.s.options.Client = c
	store := &bindingStore{Store: f.db}
	f.s.store = store
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.s.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			must(t, err)
		case <-time.After(3 * time.Second):
			t.Error("run did not join workers")
		}
	})
	deadline := time.Now().Add(3 * time.Second)
	for store.scans.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("startup did not scan")
		}
		time.Sleep(time.Millisecond)
	}
	return c, store
}
func TestBindingWorkerIdleAndCommittedDiscovery(t *testing.T) {
	f := setup(t)
	c, store := runObserved(t, f)
	rollback := errors.New("rollback")
	err := f.db.Write(t.Context(), func(ctx context.Context, _ persistence.Tx) error {
		_, err := f.s.Start(ctx, f.owner, ConnectRequest{AssistantID: "assistant", ClientID: "ding-app", ClientSecret: "fixture-secret"})
		if err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	select {
	case <-c.started:
		t.Fatal("rolled back binding started")
	case <-time.After(50 * time.Millisecond):
	}
	f.start(t)
	awaitStream(t, c.started)
	// Observe quiescence after the committed ready callback, across the former poll period.
	time.Sleep(50 * time.Millisecond)
	reads := store.reads.Load()
	time.Sleep(1200 * time.Millisecond)
	if store.scans.Load() != 1 || store.reads.Load() != reads {
		t.Fatalf("idle binding polling: scans=%d reads=%d -> %d", store.scans.Load(), reads, store.reads.Load())
	}
	must(t, f.s.Disconnect(t.Context(), f.owner, "assistant"))
	awaitStream(t, c.stopped)
	f.start(t)
	awaitStream(t, c.started)
	if store.scans.Load() != 1 {
		t.Fatal("creation rescanned all bindings")
	}
}
func TestBindingWorkerExpiryAndLiveState(t *testing.T) {
	f := setup(t)
	var enabled atomic.Bool
	enabled.Store(true)
	signal := persistence.SignalKey{Namespace: "fixture.live", Key: "assistant"}
	f.s.options.Live = func(context.Context, core.ID, core.ID) error {
		if !enabled.Load() {
			return core.NewError(core.Forbidden, "inactive")
		}
		return nil
	}
	f.s.options.LiveSignals = func(core.ID, core.ID) []persistence.SignalKey { return []persistence.SignalKey{signal} }
	f.start(t)
	c, store := runObserved(t, f)
	awaitStream(t, c.started)
	setEnabled := func(value bool) {
		enabled.Store(value)
		must(t, f.db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return tx.NotifyAfterCommit(ctx, signal) }))
	}
	setEnabled(false)
	awaitStream(t, c.stopped)
	setEnabled(true)
	awaitStream(t, c.started)
	b, err := f.s.binding(t.Context(), "owner", "chat")
	must(t, err)
	b.ExpiresAt = time.Now().Add(100 * time.Millisecond)
	must(t, f.db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return f.s.saveBinding(ctx, tx, b) }))
	awaitStream(t, c.stopped)
	_, err = f.s.binding(t.Context(), "owner", "chat")
	if !persistence.Missing(err) {
		t.Fatalf("expired binding survived: %v", err)
	}
	if store.scans.Load() != 1 {
		t.Fatal("expiry or permission change rescanned bindings")
	}
}
func TestBindingWorkerFailedCommitDoesNotDiscover(t *testing.T) {
	f := setup(t)
	c, _ := runObserved(t, f)
	failure := errors.New("commit failure")
	err := f.db.Write(t.Context(), func(ctx context.Context, _ persistence.Tx) error {
		if _, err := f.s.Start(ctx, f.owner, ConnectRequest{AssistantID: "assistant", ClientID: "ding-app", ClientSecret: "fixture-secret"}); err != nil {
			return err
		}
		f.db.FailNextCommit(failure)
		return nil
	})
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	select {
	case <-c.started:
		t.Fatal("failed commit started stream")
	case <-time.After(50 * time.Millisecond):
	}
	f.start(t)
	awaitStream(t, c.started)
}

func TestBindingCreatedDuringStartupSnapshotIsNotLost(t *testing.T) {
	f := setup(t)
	captured, release := make(chan struct{}), make(chan struct{})
	store := &bindingStore{Store: f.db, snapshot: func() { close(captured); <-release }}
	f.s.store = store
	client := &observedClient{fakeClient: f.client, started: make(chan string, 4), stopped: make(chan string, 4)}
	f.s.options.Client = client
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.s.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			must(t, err)
		case <-time.After(3 * time.Second):
			t.Error("worker did not stop")
		}
	})
	select {
	case <-captured:
	case <-time.After(3 * time.Second):
		t.Fatal("missing snapshot")
	}
	f.start(t)
	close(release)
	awaitStream(t, client.started)
	if store.scans.Load() != 1 {
		t.Fatal("startup creation required another full scan")
	}
}

func TestBindingReplacementStopsOldGeneration(t *testing.T) {
	f := setup(t)
	first, _ := f.start(t)
	client, store := runObserved(t, f)
	awaitStream(t, client.started)
	replacement, _ := f.start(t)
	if replacement.ID == first.ID {
		t.Fatal("replacement reused generation")
	}
	awaitStream(t, client.stopped)
	awaitStream(t, client.started)
	if err := f.s.receive(t.Context(), first, incoming("late", "late old generation")); err != nil {
		t.Fatal(err)
	}
	current, err := f.s.binding(t.Context(), "owner", "chat")
	must(t, err)
	if current.ID != replacement.ID || current.UserID != "" {
		t.Fatal("old stream changed new binding")
	}
	if store.scans.Load() != 1 {
		t.Fatal("replacement rescanned bindings")
	}
}
