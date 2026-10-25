package agent

import (
	"context"
	"encoding/json"

	"molr.ai/runtime/internal/contextstore"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

func storeReference(owner, assistant core.ID, kind string, id core.ID) contextstore.Reference {
	return contextstore.Reference{OwnerID: owner, AssistantID: assistant, Kind: kind, ID: string(id)}
}

type storedCheckpoint struct {
	Checkpoint
	ContextStored bool
}

func loadCheckpoint(ctx context.Context, tx persistence.Tx, owner, assistant core.ID) (Checkpoint, error) {
	row, err := loadCheckpointHeader(ctx, tx, owner, assistant)
	if err != nil {
		return Checkpoint{}, err
	}
	if err := contextstore.Load(ctx, tx, storeReference(owner, assistant, "checkpoint", assistant), &row.Context); err != nil {
		return Checkpoint{}, core.Wrap(core.Internal, "checkpoint context unavailable", err)
	}
	return row, nil
}

// Progress already owns the context at its expected revision. Read only the
// scoped header when checking that revision inside the save transaction.
func loadCheckpointHeader(ctx context.Context, tx persistence.Tx, owner, assistant core.ID) (Checkpoint, error) {
	var row storedCheckpoint
	if err := tx.Get(ctx, checkpointsBucket, persistence.Key(owner, assistant), &row); err != nil {
		return Checkpoint{}, err
	}
	if row.OwnerID != owner || row.AssistantID != assistant {
		return Checkpoint{}, core.NewError(core.Forbidden, "checkpoint unavailable")
	}
	if !row.ContextStored {
		return Checkpoint{}, core.NewError(core.FailedPrecondition, "checkpoint storage upgrade required")
	}
	return row.Checkpoint, nil
}
func saveRetry(ctx context.Context, tx persistence.Tx, r modelRetry) error {
	if err := contextstore.Save(ctx, tx, storeReference(r.OwnerID, r.AssistantID, "model_retry", r.LogicalID), map[string]any{"Inputs": r.Inputs, "Definitions": r.Definitions}); err != nil {
		return err
	}
	r.Inputs = nil
	r.Definitions = nil
	r.ContextStored = true
	return tx.Put(ctx, "agent.model_retries", persistence.Key(r.OwnerID, r.LogicalID), r)
}
func loadRetry(ctx context.Context, tx persistence.Tx, owner, logical core.ID) (modelRetry, error) {
	var r modelRetry
	if err := tx.Get(ctx, "agent.model_retries", persistence.Key(owner, logical), &r); err != nil {
		return r, err
	}
	if r.OwnerID != owner || r.AssistantID == "" || r.EventID == "" {
		return r, core.NewError(core.FailedPrecondition, "model retry storage upgrade required")
	}
	if r.ContextStored {
		var content struct {
			Inputs      json.RawMessage
			Definitions json.RawMessage
		}
		if err := contextstore.Load(ctx, tx, storeReference(owner, r.AssistantID, "model_retry", logical), &content); err != nil {
			return r, core.Wrap(core.Internal, "model retry context unavailable", err)
		}
		if err := json.Unmarshal(content.Inputs, &r.Inputs); err != nil {
			return r, err
		}
		if err := json.Unmarshal(content.Definitions, &r.Definitions); err != nil {
			return r, err
		}
	}
	return r, nil
}

// Completed event snapshots no longer participate in recovery. Receipts and
// attempt metadata remain, while the checkpoint and trace keep their own refs.
func releaseRetries(ctx context.Context, tx persistence.Tx, owner, assistant, event core.ID) error {
	q := persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner), persistence.Eq("AssistantID", assistant), persistence.Eq("ContextStored", true)}}
	if event != "" {
		q.Where = append(q.Where, persistence.Eq("EventID", event))
	}
	rows, err := tx.Query(ctx, "agent.model_retries", q)
	if err != nil {
		return err
	}
	for _, row := range rows {
		r, err := persistence.Decode[modelRetry](row)
		if err != nil {
			return err
		}
		if !r.ContextStored {
			continue
		}
		if err = contextstore.Release(ctx, tx, storeReference(owner, assistant, "model_retry", r.LogicalID)); err != nil {
			return err
		}
		r.ContextStored = false
		r.Inputs = nil
		r.Definitions = nil
		if err = tx.Put(ctx, "agent.model_retries", row.Key, r); err != nil {
			return err
		}
	}
	return nil
}
func saveDreamProgress(ctx context.Context, tx persistence.Tx, t Turn, p turnProgress) error {
	ref := storeReference(t.Principal.OwnerID, t.Event.AssistantID, "dream_progress", t.Event.ID)
	if err := contextstore.Save(ctx, tx, ref, p); err != nil {
		return err
	}
	return tx.Put(ctx, progressBucket, persistence.Key(t.Principal.OwnerID, t.Event.ID), map[string]any{"ContextStored": true, "AssistantID": t.Event.AssistantID})
}
func loadDreamProgress(ctx context.Context, store persistence.Store, t Turn) (turnProgress, error) {
	var p turnProgress
	err := store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var header struct{ ContextStored bool }
		if err := tx.Get(ctx, progressBucket, persistence.Key(t.Principal.OwnerID, t.Event.ID), &header); err != nil {
			return err
		}
		if !header.ContextStored {
			return core.NewError(core.FailedPrecondition, "dream context storage upgrade required")
		}
		if err := contextstore.Load(ctx, tx, storeReference(t.Principal.OwnerID, t.Event.AssistantID, "dream_progress", t.Event.ID), &p); err != nil {
			return core.Wrap(core.Internal, "dream context unavailable", err)
		}
		return nil
	})
	return p, err
}
