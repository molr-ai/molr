package scheduler

import (
	"errors"
	"molr.ai/runtime/internal/core"
	"time"
)

// ErrRuntimeInterrupted identifies lifecycle cancellation, not a user stop.
var ErrRuntimeInterrupted = errors.New("runtime interrupted")

// Deferred is a nonterminal result. The caller has durably saved its cursor
// and the queue transition before returning it.
type Deferred struct {
	At    time.Time
	Cause error
}

func (d *Deferred) Error() string { return "model retry scheduled" }
func (d *Deferred) Unwrap() error { return d.Cause }
func IsDeferred(err error) bool   { var d *Deferred; return errors.As(err, &d) }
func failureState(err error) State {
	if core.CodeOf(err) == core.Canceled {
		return Canceled
	}
	var known interface{ KnownFailure() bool }
	if errors.As(err, &known) && known.KnownFailure() {
		return Failed
	}
	switch core.ReasonOf(err) {
	case core.ModelNotAuthorized, core.ModelNotConfigured, core.ModelUnavailable, core.ModelPricingMissing, core.ModelBindingChanged, core.ModelContextIncompatible:
		return Failed
	}
	if core.CodeOf(err) == core.BudgetExceeded {
		return Failed
	}
	return NeedsReview
}
