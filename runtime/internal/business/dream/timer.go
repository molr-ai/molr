package dream

import (
	"context"
	"fmt"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/trigger"
)

const timers = "business.dream_timers"

// Timer progress is independent of the evidence scan cursor. A coalesced
// occurrence reserves one fixed window; expired windows never drain backlog.
type timerState struct {
	OwnerID, AssistantID core.ID
	Revision             core.Version
	Rule                 trigger.Rule
	Cursor               trigger.Cursor
	Through              time.Time
}

func dayStart(at time.Time, loc *time.Location) time.Time {
	n := at.In(loc)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc)
}
func newTimer(c Config, anchor time.Time, prior timerState) (timerState, error) {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return timerState{}, err
	}
	rule := trigger.Rule{StartAt: dayStart(anchor, loc), Timezone: c.Timezone, Recurrence: fmt.Sprintf("FREQ=DAILY;BYHOUR=%d;BYMINUTE=%d;BYSECOND=0", c.Hour, c.Minute), MissedPolicy: trigger.Coalesce}
	next, err := trigger.First(rule)
	if err != nil {
		return timerState{}, err
	}
	return timerState{OwnerID: c.OwnerID, AssistantID: c.AssistantID, Revision: prior.Revision + 1, Rule: rule, Cursor: trigger.Cursor{NextAt: next}, Through: prior.Through}, nil
}
func (s *Coordinator) timer(ctx context.Context, c Config, now time.Time) (timerState, error) {
	state, err := persistence.Load[timerState](ctx, s.store, timers, persistence.Key(c.OwnerID, c.AssistantID))
	if err == nil {
		if state.Rule.Timezone != c.Timezone {
			return newTimer(c, now, state)
		}
		return state, nil
	}
	if !persistence.Missing(err) {
		return state, err
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return state, err
	}
	// Derive the initial rule from the persisted cursor. Coalescing selects
	// the latest occurrence; startSession enforces its fixed deadline.
	firstDay := dayStart(c.NextDay, loc).AddDate(0, 0, 1)
	return newTimer(c, firstDay, timerState{})
}
func (s *Coordinator) resetTimer(ctx context.Context, c Config, now time.Time) error {
	prior, err := persistence.Load[timerState](ctx, s.store, timers, persistence.Key(c.OwnerID, c.AssistantID))
	if err != nil && !persistence.Missing(err) {
		return err
	}
	next, err := newTimer(c, now, prior)
	if err != nil {
		return err
	}
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, timers, persistence.Key(c.OwnerID, c.AssistantID), next)
	})
}
func (s *Coordinator) tickAssistant(ctx context.Context, owner, assistant core.ID, now time.Time) error {
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		c, err := s.configAt(ctx, owner, assistant, now)
		if err != nil {
			return err
		}
		// Persist the initial evidence cursor even before the first due time.
		if err := tx.Put(ctx, configs, persistence.Key(owner, assistant), c); err != nil {
			return err
		}
		if !c.Enabled {
			return nil
		}
		timer, err := s.timer(ctx, c, now)
		if err != nil {
			return err
		}
		batch, err := trigger.Due(ctx, timer.Rule, timer.Cursor, now)
		if err != nil {
			return err
		}
		loc, err := time.LoadLocation(timer.Rule.Timezone)
		if err != nil {
			return err
		}
		for _, at := range batch.Occurrences {
			inserted, err := trigger.RecordOccurrence(ctx, s.store, trigger.Occurrence{OwnerID: owner, Kind: "dream", ID: assistant, Revision: timer.Revision, At: at})
			if err != nil {
				return err
			}
			if inserted {
				timer.Through = dayStart(at, loc).UTC()
				if _, err := s.startSession(ctx, c, false, now, at); err != nil {
					return err
				}
			}
		}
		timer.Cursor = batch.Cursor
		if err := tx.Put(ctx, timers, persistence.Key(owner, assistant), timer); err != nil {
			return err
		}
		return nil
	})
}
