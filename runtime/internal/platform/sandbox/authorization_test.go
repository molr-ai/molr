package sandbox

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
)

func enableAuthorizationNotifications(t *testing.T, r *Runner) func() {
	t.Helper()
	store := quotaTestStore(t)
	signal := persistence.SignalKey{Namespace: "test.authorization", Key: "execution"}
	r.options.WatchAuthorization = func(ctx context.Context, e core.Execution, c platform.Command) (*Authorization, error) {
		watch, err := store.Watch(signal)
		if err != nil {
			return nil, err
		}
		return &Authorization{Watch: watch, Check: func(ctx context.Context) (time.Time, error) { return time.Time{}, r.options.Authorize(ctx, e, c) }}, nil
	}
	return func() {
		t.Helper()
		if err := store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return tx.NotifyAfterCommit(ctx, signal) }); err != nil {
			t.Fatal(err)
		}
	}
}

type authorizationTestWatch struct{ changes, done chan struct{} }

func (w *authorizationTestWatch) Changes() <-chan struct{} { return w.changes }
func (w *authorizationTestWatch) Done() <-chan struct{}    { return w.done }
func (w *authorizationTestWatch) Close() {
	select {
	case <-w.done:
	default:
		close(w.done)
	}
}

func TestAuthorizationObserverIdleChangesExpiryAndFailures(t *testing.T) {
	for _, mode := range []string{"revoke", "expiry", "temporary", "timeout", "closed"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				watch := &authorizationTestWatch{make(chan struct{}, 1), make(chan struct{})}
				var calls atomic.Int64
				var rejected atomic.Bool
				deadline := time.Now().Add(2 * time.Hour)
				ctx, stop, err := ObserveAuthorization(t.Context(), &Authorization{Watch: watch, Check: func(ctx context.Context) (time.Time, error) {
					calls.Add(1)
					if rejected.Load() {
						if mode == "timeout" {
							<-ctx.Done()
							return time.Time{}, core.Wrap(core.DeadlineExceeded, "lookup timed out", ctx.Err())
						}
						return time.Time{}, core.NewError(core.Forbidden, "revoked")
					}
					if mode == "expiry" || mode == "temporary" {
						if !time.Now().Before(deadline) {
							return time.Time{}, core.NewError(core.Forbidden, "time boundary")
						}
						return deadline, nil
					}
					return time.Time{}, nil
				}})
				if err != nil {
					t.Fatal(err)
				}
				defer stop()
				synctest.Wait()
				time.Sleep(time.Hour)
				synctest.Wait()
				if calls.Load() != 1 {
					t.Fatal("idle observer polled", calls.Load())
				}
				switch mode {
				case "expiry", "temporary":
					time.Sleep(time.Hour)
				case "closed":
					watch.Close()
				default:
					rejected.Store(true)
					watch.changes <- struct{}{}
				}
				synctest.Wait()
				if mode == "timeout" {
					time.Sleep(2 * time.Second)
					synctest.Wait()
				}
				select {
				case <-ctx.Done():
				default:
					t.Fatal("authorization did not stop execution")
				}
				want := "authorization_changed"
				if mode == "timeout" {
					want = "authorization_check_timeout"
				}
				if mode == "closed" {
					want = "authorization_unavailable"
				}
				if got := core.ReasonOf(context.Cause(ctx)); string(got) != want {
					t.Fatal(got, want, context.Cause(ctx))
				}
			})
		})
	}
}

func TestAuthorizationObserverReschedulesExtendedExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		watch := &authorizationTestWatch{make(chan struct{}, 1), make(chan struct{})}
		expiry := time.Now().Add(time.Minute)
		ctx, stop, err := ObserveAuthorization(t.Context(), &Authorization{Watch: watch, Check: func(context.Context) (time.Time, error) {
			if !time.Now().Before(expiry) {
				return time.Time{}, core.NewError(core.Forbidden, "expired")
			}
			return expiry, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		defer stop()
		synctest.Wait()
		expiry = expiry.Add(time.Hour)
		watch.changes <- struct{}{}
		synctest.Wait()
		time.Sleep(time.Minute)
		synctest.Wait()
		if ctx.Err() != nil {
			t.Fatal("old deadline terminated extended grant")
		}
		time.Sleep(time.Hour)
		synctest.Wait()
		if ctx.Err() == nil {
			t.Fatal("extended deadline was not enforced")
		}
	})
}
