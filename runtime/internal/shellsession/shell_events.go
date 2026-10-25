package shellsession

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"time"
)

// appendShellReminder keeps one pending reminder per assistant. Running jobs
// are immutable; an event waiting in the queue is refreshed in place.
func appendShellReminder(ctx context.Context, store persistence.Store, owner, id core.ID, now time.Time, data json.RawMessage) error {
	return shellReminder(ctx, store, owner, id, now, data, true)
}
func refreshShellReminder(ctx context.Context, store persistence.Store, owner, id core.ID, now time.Time, data json.RawMessage) error {
	return shellReminder(ctx, store, owner, id, now, data, false)
}
func shellReminder(ctx context.Context, store persistence.Store, owner, id core.ID, now time.Time, data json.RawMessage, create bool) error {
	return store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		boundary, err := persistence.Load[event.InterruptBoundary](ctx, store, "agent.interrupt_boundaries", persistence.Key(owner, id))
		if err != nil && !persistence.Missing(err) {
			return err
		}
		rows, err := persistence.Select[scheduler.Job](ctx, store, "agent.jobs", persistence.Query{Where: []persistence.Filter{persistence.Eq("Event.OwnerID", owner), persistence.Eq("Event.AssistantID", id), persistence.Eq("Event.Kind", event.ShellTasksRunning), {Field: "State", Op: "in", Value: []scheduler.State{scheduler.Queued, scheduler.Retry}}}})
		if err != nil {
			return err
		}
		for _, job := range rows {
			e := job.Event
			key := persistence.Key(owner, e.ID)
			if len(data) == 0 || boundary.Blocks(e) {
				job.State = scheduler.Canceled
				job.Version++
				if err := event.SaveJob(ctx, tx, job); err != nil {
					return err
				}
				if err := tx.Delete(ctx, "agent.events", key); err != nil {
					return err
				}
				continue
			}
			e.Details = data
			if err := tx.Put(ctx, "agent.events", key, e); err != nil {
				return err
			}
			job.Event = e
			job.Version++
			return event.SaveJob(ctx, tx, job)
		}
		if len(data) == 0 || !create {
			return nil
		}
		eid := core.ID(fmt.Sprintf("shell_reminder_%x", sha256.Sum256([]byte(fmt.Sprintf("%s/%d", id, now.UnixNano())))))
		return event.NewWriter(store).AppendAssistant(ctx, event.AssistantEvent{ID: eid, OwnerID: owner, AssistantID: id, Kind: event.ShellTasksRunning, DeliveryMode: event.AfterStep, OperationID: eid, Details: data, CreatedAt: now.UTC()})
	})
}
