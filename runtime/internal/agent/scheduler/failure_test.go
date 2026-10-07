package scheduler

import (
	"errors"
	"fmt"
	"molr.ai/runtime/internal/core"
	"testing"
	"time"
)

func TestDeferredRetainsScheduleAndCause(t *testing.T) {
	cause := core.WithReason(core.NewError(core.RateLimited, "rate limit"), core.ModelRateLimited)
	d := &Deferred{At: time.Now().Add(time.Second), Cause: cause}
	wrapped := fmt.Errorf("deferred: %w", d)
	if !IsDeferred(wrapped) || IsDeferred(cause) || IsDeferred(nil) || !errors.Is(wrapped, cause) || d.Unwrap() != cause || d.Error() == "" || core.ReasonOf(wrapped) != core.ModelRateLimited {
		t.Fatal("lost deferred result")
	}
}
