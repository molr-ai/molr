package trigger

import (
	"context"
	"time"

	"molr.ai/runtime/internal/core"
)

// Between is a read-only recurrence projection; it never changes a cursor.
// Duration includes time blocks overlapping the beginning of the range.
func Between(ctx context.Context, ruleSpec Rule, from, to time.Time, duration time.Duration) ([]time.Time, error) {
	if from.IsZero() || !to.After(from) || to.Sub(from) > 125*24*time.Hour {
		return nil, core.Invalid("range", "calendar range must be within 125 days")
	}
	times := []time.Time{}
	overlaps := func(at time.Time) bool { return !at.Before(from) || duration > 0 && at.Add(duration).After(from) }
	rule, err := recurrence(ruleSpec)
	if err != nil {
		return nil, err
	}
	if rule == nil {
		if overlaps(ruleSpec.StartAt) && ruleSpec.StartAt.Before(to) {
			times = append(times, ruleSpec.StartAt)
		}
		return times, nil
	}
	next := rule.Iterator()
	for i := 0; i < maximumRuleIterations; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		at, ok := next()
		if !ok || !at.Before(to) {
			return times, nil
		}
		if overlaps(at) {
			times = append(times, at)
			if len(times) > 4096 {
				return nil, core.Invalid("range", "too many calendar occurrences")
			}
		}
	}
	return nil, core.Invalid("range", "calendar recurrence exceeds iteration limit")
}
