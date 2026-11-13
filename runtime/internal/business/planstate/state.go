// Package planstate owns the lifecycle shared by all plan sources.
package planstate

import (
	"context"
	"encoding/json"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/persistence"
)

type State string

const (
	Active  State = "active"
	Paused  State = "paused"
	Deleted State = "deleted"
)

func Bucket(ref core.Object) string {
	switch ref.Kind {
	case "todo":
		return "business.todos"
	case "schedule":
		return "business.schedules"
	case "watch":
		return "business.watches"
	}
	return ""
}

func Load(ctx context.Context, store persistence.Store, owner core.ID, ref core.Object) (State, error) {
	if Bucket(ref) == "" {
		return "", core.Invalid("plan", "invalid plan")
	}
	row, err := persistence.Load[struct{ State State }](ctx, store, Bucket(ref), persistence.Key(owner, ref.ID))
	return row.State, err
}

// Visible rejects tombstones while leaving persisted historical references intact.
func Visible(state State) error {
	if state == Deleted {
		return core.WithReason(core.NewError(core.NotFound, "plan has been deleted"), "plan_deleted")
	}
	return nil
}

// Set changes the source revision, never the dependency-link revision.
func Set(ctx context.Context, tx persistence.Tx, owner core.ID, ref core.Object, state State, expected core.Version) error {
	if state != Active && state != Paused && state != Deleted {
		return core.Invalid("state", "invalid plan state")
	}
	var row map[string]json.RawMessage
	if err := tx.Get(ctx, Bucket(ref), persistence.Key(owner, ref.ID), &row); err != nil {
		return err
	}
	var previous State
	var version core.Version
	if err := json.Unmarshal(row["State"], &previous); err != nil {
		return err
	}
	if err := json.Unmarshal(row["Version"], &version); err != nil {
		return err
	}
	if previous == Deleted {
		if state == Deleted {
			return nil
		}
		return Visible(previous)
	}
	if version != expected {
		return core.NewError(core.Conflict, "plan changed")
	}
	row["State"], _ = json.Marshal(state)
	row["Version"], _ = json.Marshal(version + 1)
	if ref.Kind == "watch" {
		row["RunID"] = json.RawMessage(`""`)
		row["RetryAt"] = json.RawMessage(`null`)
	}
	if err := tx.Put(ctx, Bucket(ref), persistence.Key(owner, ref.ID), row); err != nil {
		return err
	}
	return RefreshHandoff(ctx, tx, owner, ref, state)
}

// Delete atomically fences dispatch and execution, preserving historical records.
// Callers authorize the source and join this operation to their mutation receipt.
func Delete(ctx context.Context, store persistence.Store, owner core.ID, ref core.Object, expected core.Version) error {
	return store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		state, err := Load(ctx, tx.Store(), owner, ref)
		if err != nil {
			return err
		}
		if state == Deleted {
			return nil
		}
		links, err := persistence.Select[struct{ Ref core.Object }](ctx, tx.Store(), "business.plan-links", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner), {Field: "Dependencies", Op: "has", Value: ref.Kind + "/" + string(ref.ID)}}})
		if err != nil {
			return err
		}
		for _, link := range links {
			state, err := Load(ctx, tx.Store(), owner, link.Ref)
			if persistence.Missing(err) {
				continue
			}
			if err != nil {
				return err
			}
			if state != Deleted {
				return core.WithReason(core.NewError(core.FailedPrecondition, "plan has dependent arrangements"), "plan_has_dependents")
			}
		}
		if err := Set(ctx, tx, owner, ref, Deleted, expected); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "business.tasks", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner), persistence.Eq("Plan.Kind", ref.Kind), persistence.Eq("Plan.ID", ref.ID)}})
		if err != nil {
			return err
		}
		taskIDs := []core.ID{}
		for _, record := range rows {
			var task struct {
				ID      core.ID
				State   string
				Version core.Version
			}
			if err := json.Unmarshal(record.Data, &task); err != nil {
				return err
			}
			taskIDs = append(taskIDs, task.ID)
			if task.State != "completed" && task.State != "failed" && task.State != "canceled" {
				var data map[string]json.RawMessage
				if err := json.Unmarshal(record.Data, &data); err != nil {
					return err
				}
				data["State"] = json.RawMessage(`"canceled"`)
				data["Version"], _ = json.Marshal(task.Version + 1)
				data["UpdatedAt"], _ = json.Marshal(time.Now().UTC())
				if err := tx.Put(ctx, "business.tasks", record.Key, data); err != nil {
					return err
				}
			}
			if err := notification.Sync(ctx, tx.Store(), owner, notification.Input{Kind: "task.failed", Source: core.Object{Kind: "task", ID: task.ID}}, false); err != nil {
				return err
			}
		}
		// Include source events without a task (watch failure notices and older scheduled jobs).
		sourceQuery := persistence.Query{Where: []persistence.Filter{persistence.Eq("Event.OwnerID", owner)}}
		if ref.Kind == "schedule" {
			sourceQuery.Where = append(sourceQuery.Where, persistence.Eq("Event.ScheduleID", ref.ID))
		} else {
			sourceQuery.Where = append(sourceQuery.Where, persistence.Eq("Event.Details.PlanID", ref.Kind+":"+string(ref.ID)))
		}
		if err := cancelJobs(ctx, tx, owner, sourceQuery); err != nil {
			return err
		}
		for start := 0; start < len(taskIDs); start += 200 {
			ids := taskIDs[start:min(start+200, len(taskIDs))]
			if err := cancelJobs(ctx, tx, owner, persistence.Query{Where: []persistence.Filter{persistence.Eq("Event.OwnerID", owner), {Field: "Event.TaskID", Op: "in", Value: ids}}}); err != nil {
				return err
			}
			operations, err := persistence.Select[struct{ ID core.ID }](ctx, tx.Store(), "agent.operations", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner), {Field: "TaskID", Op: "in", Value: ids}}, Fields: []string{"ID"}, Unordered: true})
			if err != nil {
				return err
			}
			for start := 0; start < len(operations); start += 200 {
				operationIDs := []core.ID{}
				for _, op := range operations[start:min(start+200, len(operations))] {
					operationIDs = append(operationIDs, op.ID)
				}
				if err := cancelJobs(ctx, tx, owner, persistence.Query{Where: []persistence.Filter{persistence.Eq("Event.OwnerID", owner), {Field: "Event.OperationID", Op: "in", Value: operationIDs}}}); err != nil {
					return err
				}
			}

		}
		if err := cancelPersonalRuns(ctx, tx, owner, ref); err != nil {
			return err
		}

		for _, kind := range []string{"plan.ready", "watch.failed"} {
			if err := notification.Sync(ctx, tx.Store(), owner, notification.Input{Kind: kind, Source: ref}, false); err != nil {
				return err
			}
		}
		return nil
	})
}

func cancelJobs(ctx context.Context, tx persistence.Tx, owner core.ID, q persistence.Query) error {
	jobs, err := persistence.Select[event.Job](ctx, tx.Store(), "agent.jobs", q)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		switch job.State {
		case event.Queued, event.Running, event.Retry, event.Interrupted, event.NeedsReview:
			job.State = event.Canceled
			job.Version++
			job.InterruptedAt = nil
			if err := event.SaveJob(ctx, tx, job); err != nil {
				return err
			}
		}
		if err := notification.Sync(ctx, tx.Store(), owner, notification.Input{Kind: "execution.failed", Source: core.Object{Kind: "execution", ID: job.Event.ID}}, false); err != nil {
			return err
		}
	}
	return nil
}
func cancelPersonalRuns(ctx context.Context, tx persistence.Tx, owner core.ID, ref core.Object) error {
	key := persistence.Key(owner, core.ID(ref.Kind+"-"+string(ref.ID)))
	var row map[string]json.RawMessage
	if err := tx.Get(ctx, "business.plan-links", key, &row); persistence.Missing(err) {
		return nil
	} else if err != nil {
		return err
	}
	var runs []map[string]json.RawMessage
	if raw := row["Runs"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &runs); err != nil {
			return err
		}
	}
	for _, run := range runs {
		if string(run["State"]) == `"running"` {
			run["State"] = json.RawMessage(`"canceled"`)
			run["CompletedAt"], _ = json.Marshal(time.Now().UTC())
		}
	}
	var version core.Version
	if raw := row["Version"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &version); err != nil {
			return err
		}
	}
	row["Runs"], _ = json.Marshal(runs)
	row["Version"], _ = json.Marshal(version + 1)
	row["HandoffPending"] = json.RawMessage(`false`)
	return tx.Put(ctx, "business.plan-links", key, row)
}

// RefreshHandoff keeps the candidate index in the source/link transaction.
func RefreshHandoff(ctx context.Context, tx persistence.Tx, owner core.ID, ref core.Object, state State) error {
	key := persistence.Key(owner, core.ID(ref.Kind+"-"+string(ref.ID)))
	var data map[string]json.RawMessage
	if err := tx.Get(ctx, "business.plan-links", key, &data); persistence.Missing(err) {
		return nil
	} else if err != nil {
		return err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	var links struct {
		Dependencies []core.Object
		Runs         []json.RawMessage
		Dispatched   bool
		Handoff      string
	}
	if err = json.Unmarshal(raw, &links); err != nil {
		return err
	}
	pending := state == Active && !links.Dispatched && len(links.Dependencies) > 0 && len(links.Runs) == 0 && (links.Handoff == "notify" || links.Handoff == "automatic" && ref.Kind == "todo")
	if pending {
		rows, err := tx.Query(ctx, "business.tasks", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner), persistence.Eq("Plan.Kind", ref.Kind), persistence.Eq("Plan.ID", ref.ID)}, Fields: []string{"ID"}, Limit: 1, Unordered: true})
		if err != nil {
			return err
		}
		pending = len(rows) == 0
	}
	data["HandoffPending"], _ = json.Marshal(pending)
	return tx.Put(ctx, "business.plan-links", key, data)
}
