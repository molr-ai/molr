package plan

import (
	"context"
	"encoding/json"
	"time"

	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
)

// Execution progress comes from durable jobs and turn receipts. Task.State is
// goal delivery/control state; finishing a model turn is not verified delivery.
// Only read responses use this projection, never dependency or dispatch checks.
func (s *Coordinator) executionStatuses(ctx context.Context, owner core.ID, runs []Execution) error {
	const batchSize = 256
	for start := 0; start < len(runs); start += batchSize {
		batch := runs[start:min(start+batchSize, len(runs))]
		ids := []core.ID{}
		byID := map[core.ID]*Execution{}
		projectState := map[core.ID]bool{}
		for i := range batch {
			r := &batch[i]
			if r.Task == nil {
				continue
			}
			r.TaskState = string(r.Task.State)
			projectState[r.ID] = r.Task.Plan.Kind != "watch" && r.Task.State != task.Completed && r.Task.State != task.Canceled && r.Task.State != task.Failed && r.Task.State != task.Paused
			ids = append(ids, r.ID)
			byID[r.ID] = r
		}
		if len(ids) == 0 {
			continue
		}
		jobs, err := persistence.Select[event.Job](ctx, s.store, "agent.jobs", persistence.Query{Where: []persistence.Filter{persistence.Eq("Event.OwnerID", owner), {Field: "Event.TaskID", Op: "in", Value: ids}}, Fields: []string{"Event.ID", "Event.OwnerID", "Event.AssistantID", "Event.TaskID", "Event.CreatedAt", "Event.Kind", "Event.OccurrenceAt", "Event.MessageSequence", "Event.Recovery", "State", "ExecutionID", "FailureCode"}, Unordered: true})
		if err != nil {
			return err
		}
		latest := map[core.ID]event.Job{}
		for _, j := range jobs {
			r := byID[j.Event.TaskID]
			if r == nil || j.Event.OwnerID != owner || j.Event.AssistantID != r.Task.AssistantID {
				continue
			}
			old, ok := latest[j.Event.TaskID]
			if !ok || j.Event.CreatedAt.After(old.Event.CreatedAt) || j.Event.CreatedAt.Equal(old.Event.CreatedAt) && j.Event.ID > old.Event.ID {
				latest[j.Event.TaskID] = j
			}
		}
		type timing struct {
			StartedAt time.Time
			EndedAt   *time.Time
		}
		timingKeys := []string{}
		for _, job := range latest {
			if job.ExecutionID != "" {
				timingKeys = append(timingKeys, persistence.Key(owner, job.ExecutionID))
			}
		}
		timings := map[string]timing{}
		if len(timingKeys) > 0 {
			err := s.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
				rows, err := tx.Query(ctx, "agent.turn_timing", persistence.Query{Where: []persistence.Filter{{Field: "key", Op: "in", Value: timingKeys}}, Fields: []string{"StartedAt", "EndedAt"}, Unordered: true})
				if err != nil {
					return err
				}
				for _, row := range rows {
					var value timing
					if err := json.Unmarshal(row.Data, &value); err != nil {
						return err
					}
					timings[row.Key] = value
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		eventIDs := []core.ID{}
		for _, j := range latest {
			if j.State == event.Done {
				eventIDs = append(eventIDs, j.Event.ID)
			}
		}
		type receipt struct {
			Status struct {
				State, WaitingReason      string
				ExecutionID, ActiveTaskID core.ID
			}
			Checkpoint struct {
				OwnerID, LastEventID core.ID
				SavedAt              time.Time
			}
		}
		receipts := map[core.ID]receipt{}
		if len(eventIDs) > 0 {
			rows, err := persistence.Select[receipt](ctx, s.store, "agent.handled", persistence.Query{Where: []persistence.Filter{persistence.Eq("Checkpoint.OwnerID", owner), {Field: "Checkpoint.LastEventID", Op: "in", Value: eventIDs}}, Fields: []string{"Status", "Checkpoint.OwnerID", "Checkpoint.LastEventID", "Checkpoint.SavedAt"}, Unordered: true})
			if err != nil {
				return err
			}
			for _, row := range rows {
				receipts[row.Checkpoint.LastEventID] = row
			}
		}
		boundaries := map[core.ID]event.InterruptBoundary{}
		for id, j := range latest {
			r := byID[id]
			timing := timings[persistence.Key(owner, j.ExecutionID)]
			if !timing.StartedAt.IsZero() {
				r.RunStartedAt = &timing.StartedAt
			}
			if !projectState[id] {
				continue
			}
			r.WaitingReason = string(j.FailureCode)
			if j.State != event.Done && j.State != event.Canceled && j.State != event.Interrupted {
				boundary, ok := boundaries[j.Event.AssistantID]
				if !ok {
					var err error
					boundary, err = persistence.Load[event.InterruptBoundary](ctx, s.store, "agent.interrupt_boundaries", persistence.Key(owner, j.Event.AssistantID))
					if err != nil && !persistence.Missing(err) {
						return err
					}
					boundaries[j.Event.AssistantID] = boundary
				}
				if boundary.Blocks(j.Event) {
					j.State = event.Canceled
				}
			}
			switch j.State {
			case event.Queued:
				r.State = "pending"
			case event.Running:
				r.State = "running"
			case event.Retry:
				r.State = "retry"
			case event.Failed:
				r.State = "failed"
			case event.NeedsReview:
				r.State = "needs_review"
			case event.Canceled:
				r.State = "canceled"
			case event.Interrupted:
				r.State = "interrupted"
			case event.Done:
				row, ok := receipts[j.Event.ID]
				if !ok || row.Status.ExecutionID != j.ExecutionID || row.Status.ActiveTaskID != id {
					r.State = "needs_review"
					continue
				}
				if row.Status.State == "waiting" {
					r.State = "waiting"
					r.WaitingReason = row.Status.WaitingReason
				} else if row.Status.State == "idle" {
					r.State = "executed"
					r.CompletedAt = &row.Checkpoint.SavedAt
				} else {
					r.State = "needs_review"
				}
			}
			if r.CompletedAt == nil && (r.State == "failed" || r.State == "canceled" || r.State == "interrupted" || r.State == "needs_review") {
				r.CompletedAt = timing.EndedAt
			}
		}
	}
	return nil
}
