package scheduler

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/persistence"
)

func saveJob(ctx context.Context, tx persistence.Tx, job Job) error {
	if err := event.SaveJob(ctx, tx, job); err != nil {
		return err
	}
	// Chat failures already have an assistant-blocked notice. Background runs need a durable result.
	if job.Event.Recovery != nil || job.Event.Kind == event.UserMessage || job.Event.Kind == event.Born || job.State == Running || job.State == Retry || job.State == Queued {
		return nil
	}
	return notification.Sync(ctx, tx.Store(), job.Event.OwnerID, notification.Input{Kind: "execution.failed", Code: string(job.State) + ":" + string(job.FailureCode), Source: core.Object{Kind: "execution", ID: job.Event.ID}, AssistantID: job.Event.AssistantID, Severity: "danger"}, job.State == Failed || job.State == NeedsReview)
}
