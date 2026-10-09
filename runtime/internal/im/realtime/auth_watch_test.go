package realtime

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/tests/testkit"
)

type authReadStore struct {
	persistence.Store
	reads atomic.Int64
}

func (s *authReadStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	s.reads.Add(1)
	return s.Store.Read(ctx, fn)
}

func TestAuthorizationWatchIdleConnections(t *testing.T) {
	for _, count := range []int{1, 10, 50} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "auth.db"))
			testkit.RequireNoError(t, err)
			defer db.Close()
			store := &authReadStore{Store: db}
			identity, err := auth.NewService(store, auth.Options{MasterKey: make([]byte, 32), Origin: "https://molr.example"})
			testkit.RequireNoError(t, err)
			_, err = identity.Initialize(t.Context(), auth.InitializeRequest{Username: "owner", Password: core.NewSecret("synthetic-test-password-123!")})
			testkit.RequireNoError(t, err)
			login, err := identity.Login(t.Context(), auth.LoginRequest{Username: "owner", Password: core.NewSecret("synthetic-test-password-123!")})
			testkit.RequireNoError(t, err)
			p, err := identity.Authenticate(t.Context(), auth.Authentication{Session: login.Session.Token})
			testkit.RequireNoError(t, err)
			policyService, err := policy.NewService(store, policy.Options{ValidatePrincipal: identity.ValidatePrincipal, CheckScope: identity.CheckScope, Owns: func(context.Context, core.Principal, core.Object) error { return nil }})
			testkit.RequireNoError(t, err)
			ready := make(chan struct{}, count)
			feed := NewFeed(NewStore(store), Options{Authorizer: policyService, WatchAuthorization: func(p core.Principal) (AuthorizationWatch, error) {
				w, err := identity.WatchPrincipal(p)
				return &observedAuthorization{AuthorizationWatch: w, checked: ready}, err
			}})
			stopped := make(chan error, count)
			for range count {
				sub, err := feed.Subscribe(t.Context(), p, Query{})
				testkit.RequireNoError(t, err)
				defer sub.Close()
				go func() { _, err := sub.Next(t.Context()); stopped <- err }()
			}
			for range count {
				receive(t, ready)
			}
			before := store.reads.Load()
			time.Sleep(350 * time.Millisecond)
			if delta := store.reads.Load() - before; delta != 0 {
				t.Fatalf("%d idle connections performed %d reads", count, delta)
			}
			started := time.Now()
			testkit.RequireNoError(t, identity.RevokeSession(t.Context(), p, p.SessionID))
			for range count {
				testkit.RequireCode(t, receive(t, stopped), core.Unauthenticated)
			}
			t.Logf("connections=%d idle reads=0/350ms revoke-to-close=%s", count, time.Since(started))
		})
	}
}

type observedAuthorization struct {
	AuthorizationWatch
	checked chan struct{}
}

func (w *observedAuthorization) Check(ctx context.Context) (time.Duration, error) {
	remaining, err := w.AuthorizationWatch.Check(ctx)
	select {
	case w.checked <- struct{}{}:
	default:
	}
	return remaining, err
}

type scheduledAuthorization struct {
	persistence.Watch
	check func(context.Context) (time.Duration, error)
}

func (w *scheduledAuthorization) Check(ctx context.Context) (time.Duration, error) {
	return w.check(ctx)
}

func TestAuthorizationWatchExpiryRechecksWithoutReplay(t *testing.T) {
	db, store, owner := newWakeupStore(t)
	var checks atomic.Int64
	var watcher persistence.Watch
	feed := NewFeed(store, Options{Authorizer: authorizerFunc(allow), WatchAuthorization: func(core.Principal) (AuthorizationWatch, error) {
		var err error
		watcher, err = db.Watch(persistence.SignalKey{Namespace: "auth.test", Key: "owner"})
		return &scheduledAuthorization{Watch: watcher, check: func(context.Context) (time.Duration, error) {
			if checks.Add(1) >= 3 {
				return 0, core.NewError(core.Unauthenticated, "expired")
			}
			// The second check represents activity extending the original deadline.
			return 20 * time.Millisecond, nil
		}}, err
	}})
	sub, err := feed.Subscribe(t.Context(), owner, Query{})
	testkit.RequireNoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err = sub.Next(ctx)
	testkit.RequireCode(t, err, core.Unauthenticated)
	if checks.Load() != 3 || store.reads.Load() != 1 {
		t.Fatalf("checks=%d replay=%d", checks.Load(), store.reads.Load())
	}
	testkit.RequireNoError(t, sub.Close())
	select {
	case <-watcher.Done():
	default:
		t.Fatal("authorization watch leaked")
	}
}

func TestAuthorizationCommitDuringCheckIsNotLost(t *testing.T) {
	db, store, owner := newWakeupStore(t)
	checked := make(chan struct{})
	release := make(chan struct{})
	var checks atomic.Int64
	key := persistence.SignalKey{Namespace: "auth.test", Key: "owner"}
	feed := NewFeed(store, Options{Authorizer: authorizerFunc(allow), WatchAuthorization: func(core.Principal) (AuthorizationWatch, error) {
		watcher, err := db.Watch(key)
		return &scheduledAuthorization{Watch: watcher, check: func(ctx context.Context) (time.Duration, error) {
			if checks.Add(1) == 1 {
				close(checked)
				select {
				case <-release:
				case <-ctx.Done():
					return 0, ctx.Err()
				}
				return time.Hour, nil
			}
			return 0, core.NewError(core.Unauthenticated, "revoked")
		}}, err
	}})
	sub, err := feed.Subscribe(t.Context(), owner, Query{})
	testkit.RequireNoError(t, err)
	defer sub.Close()
	result := make(chan error, 1)
	go func() { _, err := sub.Next(t.Context()); result <- err }()
	receive(t, checked)
	testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return tx.NotifyAfterCommit(ctx, key) }))
	close(release)
	testkit.RequireCode(t, receive(t, result), core.Unauthenticated)
	if store.reads.Load() != 1 {
		t.Fatal("authorization change replayed events")
	}
}

func TestAuthorizationWatchReleasedOnCancellationAndStoreClose(t *testing.T) {
	for _, action := range []string{"cancel", "close", "store"} {
		t.Run(action, func(t *testing.T) {
			db, store, owner := newWakeupStore(t)
			ready := make(chan struct{}, 1)
			var watch persistence.Watch
			feed := NewFeed(store, Options{Authorizer: authorizerFunc(allow), WatchAuthorization: func(core.Principal) (AuthorizationWatch, error) {
				var err error
				watch, err = db.Watch(persistence.SignalKey{Namespace: "auth.test", Key: "owner"})
				return &scheduledAuthorization{Watch: watch, check: func(context.Context) (time.Duration, error) { ready <- struct{}{}; return time.Hour, nil }}, err
			}})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			sub, err := feed.Subscribe(ctx, owner, Query{})
			testkit.RequireNoError(t, err)
			defer sub.Close()
			done := make(chan error, 1)
			go func() { _, err := sub.Next(t.Context()); done <- err }()
			receive(t, ready)
			switch action {
			case "cancel":
				cancel()
			case "close":
				testkit.RequireNoError(t, sub.Close())
			case "store":
				testkit.RequireNoError(t, db.Close())
			}
			if err = receive(t, done); err == nil {
				t.Fatal("closed subscription returned an event")
			}
			select {
			case <-watch.Done():
			case <-time.After(time.Second):
				t.Fatal("authorization watch leaked")
			}
		})
	}
}
