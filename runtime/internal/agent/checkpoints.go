package agent

import (
	"context"
	"encoding/json"

	"molr.ai/runtime/internal/contextstore"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/debugtrace"
	"molr.ai/runtime/internal/persistence"
)

const checkpointsBucket = "agent.checkpoints"
const statusBucket = "agent.status"

type checkpoints struct {
	store persistence.Store
	trace *debugtrace.Service
}

type checkpointCauseKey struct{}

func checkpointCause(ctx context.Context, cause string) context.Context {
	return context.WithValue(ctx, checkpointCauseKey{}, cause)
}

func NewCheckpoints(store persistence.Store) Checkpoints { return &checkpoints{store: store} }
func (c *checkpoints) Load(ctx context.Context, owner, assistant core.ID) (Checkpoint, error) {
	var out Checkpoint
	err := c.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var err error
		out, err = loadCheckpoint(ctx, tx, owner, assistant)
		return err
	})
	return out, err
}
func (c *checkpoints) Save(ctx context.Context, value Checkpoint, expected uint64) error {
	for _, id := range []core.ID{value.OwnerID, value.AssistantID, value.ExecutionID, value.LastEventID} {
		if err := id.Validate(); err != nil {
			return err
		}
	}
	if value.FormatVersion != 1 || value.Sequence == 0 || value.SavedAt.IsZero() || len(value.Context) > 256<<20 || !json.Valid(value.Context) {
		return core.Invalid("checkpoint", "unsupported checkpoint format or sequence")
	}
	return c.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var old Checkpoint
		err := tx.Get(ctx, checkpointsBucket, persistence.Key(value.OwnerID, value.AssistantID), &old)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if old.Sequence != expected || value.Sequence != expected+1 {
			return core.NewError(core.Conflict, "checkpoint sequence changed")
		}
		if err := contextstore.Save(ctx, tx, storeReference(value.OwnerID, value.AssistantID, "checkpoint", value.AssistantID), value.Context); err != nil {
			return err
		}
		stored := storedCheckpoint{Checkpoint: value, ContextStored: true}
		stored.Context = nil
		if err := tx.Put(ctx, checkpointsBucket, persistence.Key(value.OwnerID, value.AssistantID), stored); err != nil {
			return err
		}
		cause, _ := ctx.Value(checkpointCauseKey{}).(string)
		if cause == "" {
			cause = "execution_progress"
		}
		// Join the checkpoint transaction: neither a failed CAS nor a later rollback
		// can leave a successful save record or advance the workbench revision.
		c.trace.RecordCheckpoint(ctx, value.OwnerID, value.AssistantID, value.LastEventID, value.ExecutionID, map[string]any{
			"ContextBytes": len(value.Context), "ContextOmitted": true, "Sequence": value.Sequence, "PreviousSequence": expected, "SavedAt": value.SavedAt, "Cause": cause,
		})
		return nil
	})
}
