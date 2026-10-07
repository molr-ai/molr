package trigger

import (
	"context"
	"molr.ai/runtime/internal/core"
	"time"
)

// VisitUpcoming projects occurrences without changing any dispatch cursor.
// Returning false stops iteration, so callers can implement bounded pagination.
func VisitUpcoming(ctx context.Context, spec Rule, from time.Time, visit func(time.Time) bool) error {
	if from.IsZero() || visit == nil {
		return core.Invalid("from", "start time and visitor required")
	}
	rule, err := recurrence(spec)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if rule == nil {
		if !spec.StartAt.Before(from) {
			visit(spec.StartAt.UTC())
		}
		return nil
	}
	next := rule.Iterator()
	for i := 0; i < maximumRuleIterations; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		at, ok := next()
		if !ok {
			return nil
		}
		if !at.Before(from) && !visit(at.UTC()) {
			return nil
		}
	}
	return core.Invalid("recurrence", "upcoming projection exceeds iteration limit")
}
