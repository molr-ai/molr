package sqlitestore_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

func expectSignal(t *testing.T, w persistence.Watch, want bool) {
	t.Helper()
	select {
	case <-w.Changes():
		if !want {
			t.Fatal("unexpected notification")
		}
	default:
		if want {
			t.Fatal("missing notification after committed write")
		}
	}
}
func TestCommitNotificationsSavepointsAndFailures(t *testing.T) {
	for _, outcome := range []string{"commit", "rollback", "commit-failure", "canceled"} {
		t.Run(outcome, func(t *testing.T) {
			s := openStore(t)
			a := persistence.SignalKey{Namespace: "events", Key: "alice"}
			b := persistence.SignalKey{Namespace: "events", Key: "bob"}
			wa, err := s.Watch(a)
			if err != nil {
				t.Fatal(err)
			}
			defer wa.Close()
			wa2, err := s.Watch(a)
			if err != nil {
				t.Fatal(err)
			}
			defer wa2.Close()
			wb, err := s.Watch(b)
			if err != nil {
				t.Fatal(err)
			}
			defer wb.Close()
			sentinel := errors.New("rollback")
			if outcome == "commit-failure" {
				s.FailNextCommit(sentinel)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			err = s.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				if err := tx.Put(ctx, "test", "committed", true); err != nil {
					return err
				}
				if err := tx.NotifyAfterCommit(ctx, a); err != nil {
					return err
				}
				// A rolled-back duplicate must not erase the outer notification.
				err := tx.Store().Write(ctx, func(ctx context.Context, nested persistence.Tx) error {
					if err := nested.NotifyAfterCommit(ctx, a); err != nil {
						return err
					}
					if err := nested.NotifyAfterCommit(ctx, b); err != nil {
						return err
					}
					return sentinel
				})
				if !errors.Is(err, sentinel) {
					t.Fatal(err)
				}
				if err := s.Write(ctx, func(ctx context.Context, nested persistence.Tx) error { return nested.NotifyAfterCommit(ctx, a) }); err != nil {
					return err
				}
				expectSignal(t, wa, false)
				expectSignal(t, wa2, false)
				expectSignal(t, wb, false)
				if outcome == "rollback" {
					return sentinel
				}
				if outcome == "canceled" {
					cancel()
				}
				return nil
			})
			if (err == nil) != (outcome == "commit") {
				t.Fatal(err)
			}
			expectSignal(t, wa, outcome == "commit")
			expectSignal(t, wa2, outcome == "commit")
			expectSignal(t, wb, false)
			expectSignal(t, wa, false)
			if outcome == "commit" {
				value, err := persistence.Load[bool](t.Context(), s, "test", "committed")
				if err != nil || !value {
					t.Fatal("signal preceded commit", value, err)
				}
			}
		})
	}
}
func TestWatchLifecycleAndScope(t *testing.T) {
	s := openStore(t)
	key := persistence.SignalKey{Namespace: "events", Key: "owner"}
	w, err := s.Watch(key)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Watch(persistence.SignalKey{Namespace: "other", Key: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	var expired persistence.Tx
	err = s.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		expired = tx
		return tx.NotifyAfterCommit(ctx, key)
	})
	if core.CodeOf(err) != core.FailedPrecondition {
		t.Fatal(err)
	}
	if err := expired.NotifyAfterCommit(t.Context(), key); core.CodeOf(err) != core.FailedPrecondition {
		t.Fatal(err)
	}
	// Notifications coalesce even if the subscriber never drains them.
	for range 50 {
		if err := s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return tx.NotifyAfterCommit(ctx, key) }); err != nil {
			t.Fatal(err)
		}
	}
	expectSignal(t, w, true)
	expectSignal(t, w, false)
	expectSignal(t, other, false)
	w.Close()
	w.Close()
	select {
	case <-w.Done():
	default:
		t.Fatal("watch did not close")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-other.Done():
	default:
		t.Fatal("store close did not wake subscriber")
	}
	other.Close()
	if _, err := s.Watch(key); core.CodeOf(err) != core.Unavailable {
		t.Fatal(err)
	}
}
func TestConcurrentWatchCloseAndPublish(t *testing.T) {
	s := openStore(t)
	key := persistence.SignalKey{Namespace: "events", Key: "owner"}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 50 {
				w, err := s.Watch(key)
				if err != nil {
					t.Error(err)
					return
				}
				if err := s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return tx.NotifyAfterCommit(ctx, key) }); err != nil {
					t.Error(err)
				}
				w.Close()
			}
		})
	}
	wg.Wait()
}
