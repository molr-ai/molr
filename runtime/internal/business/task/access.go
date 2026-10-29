package task

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// RequireExecution checks task ownership and lifecycle for an authenticated execution.
// It joins the caller's transaction; execution identity is checked by the scheduler.
func RequireExecution(ctx context.Context, store persistence.Store, owner, id, assistant core.ID) error {
	value, err := NewRepository(store).Get(ctx, owner, id)
	if err != nil {
		return err
	}
	if value.AssistantID != assistant || value.State == Paused || value.State == Canceled || value.State == Completed || value.State == Failed {
		return core.NewError(core.Forbidden, "object access denied")
	}
	return nil
}
