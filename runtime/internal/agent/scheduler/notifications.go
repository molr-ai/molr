package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/persistence"
)

func saveJob(ctx context.Context, tx persistence.Tx, job Job) error {
	if err := event.SaveJob(ctx, tx, job); err != nil {
		return err
	}
	if job.State == Failed || job.State == NeedsReview {
		var a struct{ EmployerID core.ID }
		err := tx.Get(ctx, "business.assistants", persistence.Key(job.Event.OwnerID, job.Event.AssistantID), &a)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if a.EmployerID != "" {
			hash := sha256.Sum256([]byte("temporary-failure/" + string(job.Event.ID)))
			details, _ := json.Marshal(map[string]any{"AssistantID": job.Event.AssistantID, "Reason": job.FailureCode})
			if err := event.NewWriter(tx.Store()).AppendAssistant(ctx, event.AssistantEvent{ID: core.ID(hex.EncodeToString(hash[:])), OwnerID: job.Event.OwnerID, AssistantID: a.EmployerID, Kind: event.AssistantNotice, Details: details, CreatedAt: job.Event.CreatedAt, DeliveryMode: event.WhenIdle}); err != nil {
				return err
			}
		}
	}
	// Chat failures already have an assistant-blocked notice. Background runs need a durable result.
	if job.Event.Recovery != nil || job.Event.Kind == event.UserMessage || job.Event.Kind == event.Born || job.State == Running || job.State == Retry || job.State == Queued {
		return nil
	}
	kind := "execution.failed"
	if job.Event.Kind == event.DreamDue {
		kind = "dream.failed"
	}
	return notification.Sync(ctx, tx.Store(), job.Event.OwnerID, notification.Input{Kind: kind, Code: string(job.State) + ":" + string(job.FailureCode), Source: core.Object{Kind: "execution", ID: job.Event.ID}, AssistantID: job.Event.AssistantID, Severity: "danger"}, job.State == Failed || job.State == NeedsReview)
}
