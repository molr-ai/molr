package realtime

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
)

type observedStore struct {
	Store
	reads     atomic.Int64
	afterRead func()
}

func (s *observedStore) Replay(ctx context.Context, owner core.ID, q Query) (core.Page[Visible], error) {
	s.reads.Add(1)
	page, err := s.Store.Replay(ctx, owner, q)
	if s.afterRead != nil {
		s.afterRead()
	}
	return page, err
}

type authorizerFunc func(context.Context, core.Principal, policy.CheckRequest) error

func (f authorizerFunc) Require(ctx context.Context, p core.Principal, q policy.CheckRequest) error {
	return f(ctx, p, q)
}
func newWakeupStore(t *testing.T) (*sqlitestore.Store, *observedStore, core.Principal) {
	t.Helper()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, &observedStore{Store: NewStore(db)}, core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
}
func allow(context.Context, core.Principal, policy.CheckRequest) error { return nil }
func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
		var v T
		return v
	}
}
func TestIdleAuthorizationDoesNotReplay(t *testing.T) {
	db, store, owner := newWakeupStore(t)
	checks := make(chan struct{}, 100)
	auth := authorizerFunc(func(context.Context, core.Principal, policy.CheckRequest) error { checks <- struct{}{}; return nil })
	feed := NewFeed(store, Options{Authorizer: auth, AuthorizationInterval: time.Millisecond})
	sub, err := feed.Subscribe(t.Context(), owner, Query{})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	if store.reads.Load() != 0 {
		t.Fatal("Subscribe loaded history for validation")
	}
	result := make(chan Visible, 1)
	failed := make(chan error, 1)
	go func() { e, err := sub.Next(t.Context()); result <- e; failed <- err }()
	// Progress through many independent authorization ticks while no events exist.
	for range 15 {
		receive(t, checks)
	}
	if n := store.reads.Load(); n != 1 {
		t.Fatalf("idle stream replayed %d times", n)
	}
	// A different realtime.Store wrapper must wake the already-waiting reader.
	if err := NewStore(db).Append(t.Context(), visible(owner.OwnerID, "live")); err != nil {
		t.Fatal(err)
	}
	if e := receive(t, result); e.ID != "live" {
		t.Fatal(e)
	}
	if err := receive(t, failed); err != nil {
		t.Fatal(err)
	}
	if n := store.reads.Load(); n != 2 {
		t.Fatalf("live notification reads = %d", n)
	}
}
func TestCommitBetweenEmptyReadAndWait(t *testing.T) {
	db, store, owner := newWakeupStore(t)
	read := make(chan struct{})
	release := make(chan struct{})
	store.afterRead = func() {
		if store.reads.Load() == 1 {
			close(read)
			<-release
		}
	}
	sub, err := NewFeed(store, Options{Authorizer: authorizerFunc(allow)}).Subscribe(t.Context(), owner, Query{})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	done := make(chan error, 1)
	go func() {
		e, err := sub.Next(t.Context())
		if err == nil && e.ID != "between" {
			err = fmt.Errorf("wrong event %v", e)
		}
		done <- err
	}()
	receive(t, read)
	if err := NewStore(db).Append(t.Context(), visible(owner.OwnerID, "between")); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
}
func TestBatchedReplayAndReconnectUseDeliveredCursor(t *testing.T) {
	db, store, owner := newWakeupStore(t)
	feed := NewFeed(store, Options{Authorizer: authorizerFunc(allow)})
	sub, err := feed.Subscribe(t.Context(), owner, Query{})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	// A single outer commit yields a coalesced notification for all three pages.
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := 1; i <= 205; i++ {
			if err := NewStore(tx.Store()).Append(ctx, visible(owner.OwnerID, core.ID(fmt.Sprintf("e%03d", i)))); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var last Visible
	for i := 1; i <= 205; i++ {
		last, err = sub.Next(t.Context())
		if err != nil || last.Sequence != uint64(i) {
			t.Fatal(last, err)
		}
		if i == 1 {
			cursor, _ := Cursor(owner.OwnerID, nil, last.Sequence)
			second, err := feed.Subscribe(t.Context(), owner, Query{After: cursor})
			if err != nil {
				t.Fatal(err)
			}
			e, err := second.Next(t.Context())
			second.Close()
			if err != nil || e.Sequence != 2 {
				t.Fatal("prefetch advanced reconnect cursor", e, err)
			}
		}
	}
	if n := store.reads.Load(); n != 4 {
		t.Fatalf("expected 3 batch reads plus one reconnect, got %d", n)
	}
}
func TestBufferedEventsStillCheckRevocationAndStoreClose(t *testing.T) {
	for _, closeStore := range []bool{false, true} {
		t.Run(fmt.Sprint(closeStore), func(t *testing.T) {
			db, store, owner := newWakeupStore(t)
			var revoked atomic.Bool
			auth := authorizerFunc(func(context.Context, core.Principal, policy.CheckRequest) error {
				if revoked.Load() {
					return core.NewError(core.Unauthenticated, "revoked")
				}
				return nil
			})
			for _, id := range []core.ID{"first", "second"} {
				if err := store.Append(t.Context(), visible(owner.OwnerID, id)); err != nil {
					t.Fatal(err)
				}
			}
			sub, err := NewFeed(store, Options{Authorizer: auth}).Subscribe(t.Context(), owner, Query{})
			if err != nil {
				t.Fatal(err)
			}
			defer sub.Close()
			if _, err := sub.Next(t.Context()); err != nil {
				t.Fatal(err)
			}
			want := core.Unauthenticated
			if closeStore {
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
				want = core.Unavailable
			} else {
				revoked.Store(true)
			}
			if _, err := sub.Next(t.Context()); core.CodeOf(err) != want {
				t.Fatal(err)
			}
		})
	}
}
func TestTopicReplayMergesBoundedPages(t *testing.T) {
	_, store, owner := newWakeupStore(t)
	for i := 1; i <= 80; i++ {
		e := visible(owner.OwnerID, core.ID(fmt.Sprintf("e%03d", i)))
		e.Topic = []string{"skip", "a", "b", "skip"}[i%4]
		if err := store.Append(t.Context(), e); err != nil {
			t.Fatal(err)
		}
	}
	q := Query{Topics: []string{"b", "a"}, Page: core.PageRequest{Limit: 3}}
	var sequences []uint64
	for {
		page, err := store.Replay(t.Context(), owner.OwnerID, q)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page.Items {
			sequences = append(sequences, e.Sequence)
		}
		if page.NextCursor == "" {
			break
		}
		q.Page.Cursor = page.NextCursor
	}
	if len(sequences) != 40 {
		t.Fatal(sequences)
	}
	for i, s := range sequences {
		if s != uint64((i/2)*4+i%2+1) {
			t.Fatal(sequences)
		}
	}
	q.Topics = make([]string, 17)
	if _, err := store.Replay(t.Context(), owner.OwnerID, q); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal(err)
	}
}

func TestCanceledContextRemovesWatchWithoutNext(t *testing.T) {
	_, store, owner := newWakeupStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	sub, err := NewFeed(store, Options{Authorizer: authorizerFunc(allow)}).Subscribe(ctx, owner, Query{})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	receive(t, sub.(*subscription).watch.Done())
	sub.Close()
}
func TestWaitingNextStopsWhenStoreCloses(t *testing.T) {
	db, store, owner := newWakeupStore(t)
	read := make(chan struct{}, 1)
	store.afterRead = func() { read <- struct{}{} }
	sub, err := NewFeed(store, Options{Authorizer: authorizerFunc(allow)}).Subscribe(t.Context(), owner, Query{})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	done := make(chan error, 1)
	go func() { _, err := sub.Next(t.Context()); done <- err }()
	receive(t, read)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, done); core.CodeOf(err) != core.Unavailable {
		t.Fatal(err)
	}
}
func TestIdempotentAppendDoesNotNotify(t *testing.T) {
	_, store, owner := newWakeupStore(t)
	w, err := store.Watch(owner.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	e := visible(owner.OwnerID, "same")
	e.CreatedAt = e.CreatedAt.UTC()
	if err := store.Append(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	receive(t, w.Changes())
	if err := store.Append(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.Changes():
		t.Fatal("idempotent append notified")
	default:
	}
}

func TestReconnectAfterReopenAndLongestTopicCursor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.db")
	db, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	topics := make([]string, 16)
	for i := range topics {
		topics[i] = fmt.Sprintf("%02d", i) + strings.Repeat("\x01", 126)
	}
	for i := 1; i <= 3; i++ {
		e := visible("owner", core.ID(fmt.Sprintf("e%d", i)))
		e.Topic = topics[0]
		if err := store.Append(t.Context(), e); err != nil {
			t.Fatal(err)
		}
	}
	cursor, err := Cursor("owner", topics, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	page, err := NewStore(db).Replay(t.Context(), "owner", Query{After: cursor, Topics: topics})
	if err != nil || len(page.Items) != 2 || page.Items[0].Sequence != 2 {
		t.Fatal(page, err)
	}
}
