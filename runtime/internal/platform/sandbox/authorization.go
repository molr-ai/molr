package sandbox

import (
	"context"
	"errors"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
)

// Authorization observes committed policy changes. Check returns the next time
// boundary (grant/temporary quota expiry), or zero when only changes can invalidate it.
type Authorization struct {
	persistence.Watch
	Check func(context.Context) (time.Time, error)
}

func (r *Runner) observeAuthorization(parent context.Context, e core.Execution, c platform.Command) (context.Context, context.CancelFunc, error) {
	if r.options.WatchAuthorization == nil {
		ctx, stop := context.WithCancel(parent)
		return ctx, stop, nil
	}
	watch, err := r.options.WatchAuthorization(parent, e, c)
	if err != nil {
		return nil, nil, err
	}
	return ObserveAuthorization(parent, watch)
}

// ObserveAuthorization owns the subscription until cancellation and revalidates
// only after changes or a time boundary. The caller must stop it after execution.
func ObserveAuthorization(parent context.Context, watch *Authorization) (context.Context, context.CancelFunc, error) {
	ctx, cancel := context.WithCancelCause(parent)
	stop := func() { cancel(context.Canceled) }
	check := func() (time.Time, error) {
		checkCtx, finish := context.WithTimeout(ctx, 2*time.Second)
		defer finish()
		return watch.Check(checkCtx)
	}
	next, err := check()
	if err != nil {
		watch.Close()
		stop()
		return nil, nil, err
	}
	go func() {
		defer watch.Close()
		timer := time.NewTimer(time.Hour)
		defer timer.Stop()
		for {
			timer.Stop()
			var expiry <-chan time.Time
			if !next.IsZero() {
				timer.Reset(max(time.Until(next), time.Nanosecond))
				expiry = timer.C
			}
			select {
			case <-ctx.Done():
				return
			case <-watch.Done():
				cancel(core.WithReason(core.NewError(core.Unavailable, "authorization observer closed"), "authorization_unavailable"))
				return
			case <-watch.Changes():
			case <-expiry:
			}
			next, err = check()
			if err != nil {
				cancel(authorizationFailure(err))
				return
			}
		}
	}()
	return ctx, stop, nil
}

func authorizationFailure(err error) error {
	reason := core.ReasonOf(err)
	if reason == "" {
		switch core.CodeOf(err) {
		case core.Forbidden, core.Unauthenticated, core.NotFound:
			reason = "authorization_changed"
		case core.DeadlineExceeded:
			reason = "authorization_check_timeout"
		default:
			reason = "authorization_check_failed"
		}
	}
	return core.WithReason(core.Wrap(core.CodeOf(err), "execution authorization check failed", err), reason)
}

func executionStopReason(err error) string {
	if reason := core.ReasonOf(err); reason != "" {
		return string(reason)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return string(core.ReasonOf(authorizationFailure(err)))
}
