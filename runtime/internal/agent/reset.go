package agent

import (
	"context"
	"encoding/json"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

type resetReceipt struct {
	ExpectedSequence uint64
	Sequence         uint64
}

// ResetContext clears private execution state without deleting conversation,
// memory, grants or billing evidence. Existing jobs are canceled, not replayed.
func (e *Engine) ResetContext(ctx context.Context, p core.Principal, id core.ID, mutation core.Mutation, expected uint64) error {
	if err := p.RequireOwner(); err != nil {
		return err
	}
	if err := id.Validate(); err != nil {
		return err
	}
	if err := mutation.Validate(false); err != nil {
		return err
	}
	key := persistence.Key(p.OwnerID, id)
	var canceledExecution core.ID
	changed := false
	err := e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := e.authorize(ctx, p, id, policy.Write); err != nil {
			return err
		}
		a, err := persistence.Load[struct {
			OwnerID core.ID
			State   string
		}](ctx, e.store, "business.assistants", key)
		if err != nil {
			return err
		}
		if a.OwnerID != p.OwnerID {
			return core.NewError(core.Forbidden, "assistant unavailable")
		}
		if a.State != "active" {
			return core.NewError(core.FailedPrecondition, "assistant is not active")
		}
		receiptKey := persistence.Key(p.OwnerID, core.ID(string(id)+"/"+mutation.IdempotencyKey))
		var previous resetReceipt
		err = tx.Get(ctx, "agent.context_reset_receipts", receiptKey, &previous)
		if err == nil {
			if previous.ExpectedSequence != expected {
				return core.NewError(core.Conflict, "reset payload changed")
			}
			return nil
		}
		if !persistence.Missing(err) {
			return err
		}
		checkpoint, err := e.checkpoints.Load(ctx, p.OwnerID, id)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if checkpoint.Sequence != expected {
			return core.NewError(core.Conflict, "checkpoint changed")
		}
		status, err := persistence.Load[Status](ctx, e.store, statusBucket, key)
		if err != nil {
			return err
		}
		canceledExecution = status.ExecutionID
		// Lifecycle cancels running/queued/retry/review jobs and materializes pending
		// events as canceled jobs in this same transaction. Restore accepts new work.
		if err = e.Lifecycle(ctx, p, id, "stop"); err != nil {
			return err
		}
		if err = e.Lifecycle(ctx, p, id, "restore"); err != nil {
			return err
		}
		now := e.options.Now().UTC()
		resetID := stableID(string(id), "context-reset", mutation.IdempotencyKey)
		checkpoint = Checkpoint{OwnerID: p.OwnerID, AssistantID: id, ExecutionID: resetID, LastEventID: resetID, FormatVersion: 1, Sequence: expected + 1, Context: json.RawMessage(`{"Inputs":[]}`), SavedAt: now}
		if err = e.checkpoints.Save(checkpointCause(ctx, "context_reset"), checkpoint, expected); err != nil {
			return err
		}
		if err = releaseRetries(ctx, tx, p.OwnerID, id, ""); err != nil {
			return err
		}
		// Also fence delayed deliveries of events created before the reset, including
		// messages whose IM outbox has not yet reached the scheduler.
		if err = tx.Put(ctx, "agent.context_reset_at", key, now); err != nil {
			return err
		}
		if err = tx.Put(ctx, "agent.context_reset_receipts", receiptKey, resetReceipt{expected, checkpoint.Sequence}); err != nil {
			return err
		}
		status, err = persistence.Load[Status](ctx, e.store, statusBucket, key)
		if err != nil {
			return err
		}
		status.Context = nil
		if err = e.saveStatus(ctx, tx, key, status); err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err == nil && changed {
		e.mu.Lock()
		active := e.active[key]
		e.mu.Unlock()
		// Never cancel a new execution that started after the reset committed.
		if active.id == canceledExecution && active.cancel != nil {
			active.cancel()
		}
	}
	return err
}

func (e *Engine) checkContextEpoch(ctx context.Context, owner, assistant core.ID, created time.Time) error {
	reset, err := persistence.Load[time.Time](ctx, e.store, "agent.context_reset_at", persistence.Key(owner, assistant))
	if persistence.Missing(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !created.After(reset) {
		return core.NewError(core.Canceled, "event predates context reset")
	}
	return nil
}
