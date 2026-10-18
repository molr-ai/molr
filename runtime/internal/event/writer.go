package event

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

const eventsBucket = "agent.events"
const sourcesBucket = "agent.event_sources"

type writer struct{ store persistence.Store }

// NewWriter persists assistant inputs and their source identity in the caller's
// transaction, together with the initial durable job.
func NewWriter(store persistence.Store) Writer { return &writer{store: store} }
func (w *writer) AppendAssistant(ctx context.Context, e AssistantEvent) error {
	e.DeliveryMode = e.Delivery()
	if err := e.Validate(); err != nil {
		return err
	}
	if w.store == nil {
		return core.NewError(core.FailedPrecondition, "event storage unavailable")
	}
	source := struct {
		Assistant core.ID
		Kind      Kind
		ID        core.ID
		At        string
		Task      core.ID
		Version   core.Version
	}{Assistant: e.AssistantID, Kind: e.Kind}
	switch e.Kind {
	case Born:
		// Birth belongs to the assistant identity, independent of event ID/time.
		source.ID = e.AssistantID
	case UserMessage:
		source.ID = e.MessageID
	case ScheduleDue:
		source.ID = e.ScheduleID
		source.Version = e.ScheduleVersion
		source.At = e.OccurrenceAt.UTC().Format(time.RFC3339Nano)
	case ShellTaskFinished, ShellTasksRunning, OperationCompleted:
		source.ID = e.OperationID
	// Each dream batch and explicitly authorized retry is a distinct event.
	case DreamDue, ResumeRequested, BrowserControlReturned, TerminalControlReturned, UserStopped, ContextCompressionRequested:
		source.ID = e.ID
		source.Task = e.TaskID
	}
	data, _ := json.Marshal(source)
	sum := sha256.Sum256(data)
	sourceKey := persistence.Key(e.OwnerID, core.ID(hex.EncodeToString(sum[:])))
	return w.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var existing core.ID
		err := tx.Get(ctx, sourcesBucket, sourceKey, &existing)
		if err == nil {
			return nil
		}
		if !persistence.Missing(err) {
			return err
		}
		key := persistence.Key(e.OwnerID, e.ID)
		var prior AssistantEvent
		if err = tx.Get(ctx, eventsBucket, key, &prior); err == nil {
			return core.NewError(core.Conflict, "event ID already exists")
		}
		if !persistence.Missing(err) {
			return err
		}
		if err = tx.Put(ctx, eventsBucket, key, e); err != nil {
			return err
		}
		if err := tx.Put(ctx, sourcesBucket, sourceKey, e.ID); err != nil {
			return err
		}
		if e.Kind == UserStopped {
			return nil
		}
		// Stop can fence an IM input before its outbox event is consumed.
		// Materializing that source must never revive the canceled job.
		var job Job
		if err := tx.Get(ctx, "agent.jobs", key, &job); err == nil {
			return nil
		} else if !persistence.Missing(err) {
			return err
		}
		return SaveJob(ctx, tx, Job{Event: e, State: Queued, NotBefore: e.CreatedAt, Version: 1})
	})
}
