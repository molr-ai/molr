// Package trigger provides business-neutral recurrence calculation and durable
// occurrence deduplication. It creates no plans, tasks or assistant events.
package trigger

import (
	"context"
	"strings"
	"time"

	"github.com/teambition/rrule-go"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

const normalDispatchGrace = time.Minute
const maximumRuleIterations = 100000

type MissedPolicy string

const (
	Skip     MissedPolicy = "skip"
	Coalesce MissedPolicy = "coalesce"
	CatchUp  MissedPolicy = "catch_up"
)

type Rule struct {
	StartAt      time.Time
	Timezone     string
	Recurrence   string
	MissedPolicy MissedPolicy
	MaxCatchUp   int
}

func (t Rule) Validate() error {
	if t.StartAt.IsZero() || t.Timezone == "" || len(t.Recurrence) > 1024 {
		return core.Invalid("trigger", "start, timezone and bounded recurrence required")
	}
	if _, err := time.LoadLocation(t.Timezone); err != nil {
		return core.Invalid("timezone", "unknown timezone")
	}
	switch t.MissedPolicy {
	case Skip, Coalesce:
		if t.MaxCatchUp != 0 {
			return core.Invalid("max_catch_up", "only valid for catch_up")
		}
	case CatchUp:
		if t.MaxCatchUp < 1 || t.MaxCatchUp > 100 {
			return core.Invalid("max_catch_up", "must be between 1 and 100")
		}
	default:
		return core.Invalid("missed_policy", "required")
	}
	return nil
}

// Cursor is persisted by each adapter in the same transaction as its effects.
// Resumed means NextAt and Consumed came from this exact rule revision.
type Cursor struct {
	NextAt   *time.Time
	Consumed int
	Resumed  bool
}
type Batch struct {
	Occurrences []time.Time
	Cursor      Cursor
}

// Due selects bounded missed occurrences and advances beyond now. Coalesce
// wakes once after downtime; CatchUp keeps the most recent MaxCatchUp points.
func Due(ctx context.Context, rule Rule, cursor Cursor, now time.Time) (Batch, error) {
	if now.IsZero() || cursor.Consumed < 0 {
		return Batch{}, core.Invalid("cursor", "valid time and nonnegative consumed count required")
	}
	if err := persistence.CheckContext(ctx); err != nil {
		return Batch{}, err
	}
	selected, next, consumed, err := dueOccurrences(ctx, rule, cursor, now)
	if err != nil {
		return Batch{}, err
	}
	if cursor.Resumed {
		consumed += cursor.Consumed
	}
	return Batch{Occurrences: selected, Cursor: Cursor{NextAt: next, Consumed: consumed, Resumed: true}}, nil
}

// recurrence validates execution semantics separately from Rule.Validate's
// public shape checks. DTSTART comes only from StartAt and Timezone.
func recurrence(trigger Rule) (*rrule.RRule, error) {
	if err := trigger.Validate(); err != nil {
		return nil, err
	}
	if trigger.Recurrence == "" {
		return nil, nil
	}
	trigger.Recurrence = strings.ToUpper(strings.TrimSpace(trigger.Recurrence))
	if strings.ContainsAny(trigger.Recurrence, "\r\n") || strings.Contains(trigger.Recurrence, "DTSTART") {
		return nil, core.Invalid("recurrence", "use StartAt and Timezone for DTSTART")
	}
	location, err := time.LoadLocation(trigger.Timezone)
	if err != nil {
		return nil, err
	}
	options, err := rrule.StrToROptionInLocation(trigger.Recurrence, location)
	if err != nil {
		return nil, core.Invalid("recurrence", "valid RFC5545 RRULE required")
	}
	if (strings.Contains(trigger.Recurrence, "COUNT=") && options.Count <= 0) || (strings.Contains(trigger.Recurrence, "INTERVAL=") && options.Interval <= 0) || (options.Count > 0 && !options.Until.IsZero()) {
		return nil, core.Invalid("recurrence", "positive COUNT/INTERVAL and mutually exclusive COUNT/UNTIL required")
	}
	options.Dtstart = trigger.StartAt.In(location)
	rule, err := rrule.NewRRule(*options)
	if err != nil {
		return nil, core.Invalid("recurrence", "invalid recurrence options")
	}
	return rule, nil
}
func First(trigger Rule) (*time.Time, error) {
	rule, err := recurrence(trigger)
	if err != nil {
		return nil, err
	}
	first := trigger.StartAt
	if rule != nil {
		next := rule.Iterator()
		var exists bool
		first, exists = next()
		if !exists {
			return nil, nil
		}
	}
	return &first, nil
}

func dueOccurrences(ctx context.Context, ruleSpec Rule, cursor Cursor, now time.Time) ([]time.Time, *time.Time, int, error) {
	rule, err := recurrence(ruleSpec)
	if err != nil {
		return nil, nil, 0, err
	}
	if cursor.NextAt == nil {
		return nil, nil, 0, nil
	}
	selected := []time.Time{}
	choose := func(at time.Time) {
		switch ruleSpec.MissedPolicy {
		case Skip:
			if !at.Before(now.Add(-normalDispatchGrace)) {
				selected = append(selected, at)
			}
		case Coalesce:
			selected = []time.Time{at}
		case CatchUp:
			if len(selected) == ruleSpec.MaxCatchUp {
				copy(selected, selected[1:])
				selected = selected[:len(selected)-1]
			}
			selected = append(selected, at)
		}
	}
	if rule == nil {
		if !cursor.NextAt.After(now) {
			choose(*cursor.NextAt)
			return selected, nil, 0, nil
		}
		return selected, cursor.NextAt, 0, nil
	}
	consumed := 0
	if cursor.Resumed {
		options := rule.Options
		options.Dtstart = cursor.NextAt.In(rule.GetDTStart().Location())
		if options.Count > 0 {
			options.Count -= cursor.Consumed
			if options.Count <= 0 {
				return selected, nil, 0, nil
			}
		}
		resumed, err := rrule.NewRRule(options)
		if err != nil {
			return nil, nil, 0, err
		}
		rule = resumed
	}
	next := rule.Iterator()
	for count := 0; count < maximumRuleIterations; count++ {
		if err := persistence.CheckContext(ctx); err != nil {
			return nil, nil, 0, err
		}
		at, exists := next()
		if !exists {
			return selected, nil, consumed, nil
		}
		if at.Before(*cursor.NextAt) {
			consumed++
			continue
		}
		if at.After(now) {
			return selected, &at, consumed, nil
		}
		choose(at)
		consumed++
	}
	return nil, nil, 0, core.NewError(core.FailedPrecondition, "recurrence exceeds bounded catch-up calculation")
}
