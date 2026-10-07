package schedule

import (
	"context"
	"molr.ai/runtime/internal/trigger"
	"time"
)

// CalendarTimes projects the shared recurrence without advancing paused plans.
func CalendarTimes(ctx context.Context, plan Schedule, from, to time.Time) ([]time.Time, error) {
	duration := time.Duration(0)
	if plan.EndAt != nil {
		duration = plan.EndAt.Sub(plan.Trigger.StartAt)
	}
	return trigger.Between(ctx, trigger.Rule(plan.Trigger), from, to, duration)
}
