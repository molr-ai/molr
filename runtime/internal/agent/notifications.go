package agent

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/persistence"
	"strings"
)

func (e *Engine) saveStatus(ctx context.Context, tx persistence.Tx, key string, status Status) error {
	if err := tx.Put(ctx, statusBucket, key, status); err != nil {
		return err
	}
	// Starting an attempt does not prove the previous blocking problem recovered.
	if status.State == Working || status.State == Dreaming {
		return nil
	}
	return notification.Sync(ctx, e.store, core.ID(strings.SplitN(key, "/", 2)[0]), notification.Input{Kind: "assistant.waiting", Code: status.WaitingReason, Source: core.Object{Kind: "assistant", ID: status.AssistantID}, AssistantID: status.AssistantID}, status.State == Waiting && status.WaitingReason != "permission_pending")
}
