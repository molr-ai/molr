package schedule

import (
	"context"
	"errors"
	"log/slog"
	"molr.ai/runtime/internal/business/planstate"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/trigger"
)

func firstOccurrence(t Trigger) (*time.Time, error) { return trigger.First(trigger.Rule(t)) }

// Dispatcher advances persisted schedules and appends real source events. It
// never writes chat messages and serializes occurrence/event/progress changes.
type Dispatcher struct {
	store persistence.Store
	repo  Repository
	Ready func(context.Context, core.ID, core.ID) (bool, error)
	Start func(context.Context, Schedule, time.Time) (core.ID, error)
}

func NewDispatcher(store persistence.Store) *Dispatcher {
	return &Dispatcher{store: store, repo: NewRepository(store)}
}

type dispatchProgress struct {
	Version  core.Version
	Consumed int
}

func (d *Dispatcher) Tick(ctx context.Context, now time.Time) error {
	if now.IsZero() {
		return core.Invalid("now", "dispatch time required")
	}
	due, err := d.repo.Due(ctx, now, 200)
	if err != nil {
		return err
	}
	var failures []error
	for _, candidate := range due {
		progress, progressErr := persistence.Load[dispatchProgress](ctx, d.store, "business.schedule-progress", persistence.Key(candidate.OwnerID, candidate.ID))
		if progressErr != nil && !persistence.Missing(progressErr) {
			failures = append(failures, progressErr)
			continue
		}
		if progress.Version != candidate.Version {
			progress = dispatchProgress{}
		}
		batch, err := trigger.Due(ctx, trigger.Rule(candidate.Trigger), trigger.Cursor{NextAt: candidate.NextAt, Consumed: progress.Consumed, Resumed: progress.Version == candidate.Version}, now)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		err = d.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			current, err := d.repo.Get(ctx, candidate.OwnerID, candidate.ID)
			if persistence.Missing(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if current.State != planstate.Active || current.Version != candidate.Version || current.NextAt == nil || !current.NextAt.Equal(*candidate.NextAt) {
				return nil
			}
			if d.Ready != nil {
				ready, err := d.Ready(ctx, current.OwnerID, current.ID)
				if err != nil {
					return err
				}
				if !ready {
					return nil
				}
			}
			if current.Kind == AssistantSchedule {
				var assistant struct {
					OwnerID core.ID
					State   string
				}
				err = tx.Get(ctx, "business.assistants", persistence.Key(current.OwnerID, current.AssistantID), &assistant)
				if err != nil && !persistence.Missing(err) {
					return err
				}
				if err != nil || assistant.OwnerID != current.OwnerID || assistant.State != "active" {
					current.State = planstate.Paused
					current.NextAt = nil
					current.Version++
					return d.repo.Save(ctx, current, candidate.Version)
				}
			} else if current.Kind != Personal {
				slog.ErrorContext(ctx, "unsupported schedule kind", "owner_id", current.OwnerID, "schedule_id", current.ID, "kind", current.Kind)
				return nil
			}
			for _, at := range batch.Occurrences {
				inserted, err := d.repo.RecordOccurrence(ctx, current.OwnerID, Occurrence{ScheduleID: current.ID, ScheduleVersion: current.Version, DueAt: at})
				if err != nil {
					return err
				}
				if !inserted || current.Kind == Personal {
					continue
				}
				var taskID core.ID
				if d.Start != nil {
					taskID, err = d.Start(ctx, current, at)
					if err != nil {
						return err
					}
				}
				if err = event.NewWriter(tx.Store()).AppendAssistant(ctx, event.AssistantEvent{ID: newID(), OwnerID: current.OwnerID, AssistantID: current.AssistantID, Kind: event.ScheduleDue, DeliveryMode: event.WhenIdle, TaskID: taskID, ScheduleID: current.ID, ScheduleVersion: current.Version, OccurrenceAt: at, CreatedAt: now}); err != nil {
					return err
				}
			}
			current.NextAt = batch.Cursor.NextAt
			current.Version++
			if err := d.repo.Save(ctx, current, candidate.Version); err != nil {
				return err
			}
			return tx.Put(ctx, "business.schedule-progress", persistence.Key(current.OwnerID, current.ID), dispatchProgress{Version: current.Version, Consumed: batch.Cursor.Consumed})
		})
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// NextWake returns the nearest runnable occurrence. Blocked overdue schedules
// wait for dependency changes instead of creating an immediate timer loop.
func (d *Dispatcher) NextWake(ctx context.Context, now time.Time) (time.Time, error) {
	rows, err := persistence.Select[Schedule](ctx, d.store, "business.schedules", persistence.Query{Where: []persistence.Filter{persistence.Eq("State", planstate.Active)}, Fields: []string{"ID", "OwnerID", "Kind", "NextAt"}, Unordered: true})
	if err != nil {
		return time.Time{}, err
	}
	var next time.Time
	for _, s := range rows {
		if s.NextAt == nil || (s.Kind != AssistantSchedule && s.Kind != Personal) {
			continue
		}
		if !s.NextAt.After(now) && d.Ready != nil {
			ready, err := d.Ready(ctx, s.OwnerID, s.ID)
			if persistence.Missing(err) {
				continue
			}
			if err != nil {
				return next, err
			}
			if !ready {
				continue
			}
		}
		if next.IsZero() || s.NextAt.Before(next) {
			next = *s.NextAt
		}
	}
	return next, nil
}
