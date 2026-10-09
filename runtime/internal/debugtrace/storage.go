package debugtrace

import (
	"context"
	"encoding/json"

	"molr.ai/runtime/internal/contextstore"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type storedPayload struct {
	Payload
	ContextStored bool
}

func payloadReference(owner, assistant, id core.ID, sequence int) contextstore.Reference {
	return contextstore.Reference{OwnerID: owner, AssistantID: assistant, Kind: "debug_payload", ID: payloadKey(owner, id, sequence)}
}
func savePayload(ctx context.Context, tx persistence.Tx, run Run, sequence int, payload Payload) error {
	row := storedPayload{Payload: payload}
	if len(payload.Data) > 0 {
		if err := contextstore.Save(ctx, tx, payloadReference(run.OwnerID, run.AssistantID, run.ID, sequence), payload.Data); err != nil {
			return err
		}
		row.Data = nil
		row.ContextStored = true
	}
	return tx.Put(ctx, "debug.payloads", payloadKey(run.OwnerID, run.ID, sequence), row)
}
func loadPayload(ctx context.Context, tx persistence.Tx, run Run, sequence int) (Payload, error) {
	var row storedPayload
	if err := tx.Get(ctx, "debug.payloads", payloadKey(run.OwnerID, run.ID, sequence), &row); err != nil {
		return Payload{}, err
	}
	if !row.ContextStored && len(row.Data) > 0 && string(row.Data) != "null" {
		return Payload{}, core.NewError(core.FailedPrecondition, "debug payload storage upgrade required")
	}
	if row.ContextStored {
		if err := contextstore.Load(ctx, tx, payloadReference(run.OwnerID, run.AssistantID, run.ID, sequence), &row.Data); err != nil {
			return Payload{}, err
		}
	}
	return row.Payload, nil
}
func loadCheckpoint(ctx context.Context, tx persistence.Tx, owner, assistant core.ID) (json.RawMessage, error) {
	var row map[string]json.RawMessage
	if err := tx.Get(ctx, "agent.checkpoints", persistence.Key(owner, assistant), &row); err != nil {
		return nil, err
	}
	if string(row["ContextStored"]) != "true" {
		return nil, core.NewError(core.FailedPrecondition, "checkpoint storage upgrade required")
	}
	var body json.RawMessage
	if err := contextstore.Load(ctx, tx, contextstore.Reference{OwnerID: owner, AssistantID: assistant, Kind: "checkpoint", ID: string(assistant)}, &body); err != nil {
		return nil, err
	}
	row["Context"] = body
	delete(row, "ContextStored")
	return json.Marshal(row)
}
