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
	// Model detail is a projection of the stored context, not a second copy
	// of the response text, calls or provider state.
	if sequence > 0 && sequence <= len(run.Steps) && run.Steps[sequence-1].Kind == "model.complete" && len(row.Data) > 0 {
		var data map[string]json.RawMessage
		if err := json.Unmarshal(row.Data, &data); err != nil {
			return Payload{}, err
		}
		if raw, ok := data["Inputs"]; ok {
			var inputs []map[string]json.RawMessage
			if err := json.Unmarshal(raw, &inputs); err != nil {
				return Payload{}, err
			}
			if len(inputs) == 0 || string(inputs[len(inputs)-1]["Role"]) != `"assistant"` {
				return Payload{}, core.NewError(core.Internal, "model output context unavailable")
			}
			output := inputs[len(inputs)-1]
			for _, field := range []string{"Text", "Calls", "ProviderState"} {
				if value, ok := output[field]; ok {
					data[field] = value
				}
			}
			if data["Text"] == nil {
				data["Text"] = json.RawMessage(`""`)
			}
			delete(data, "Inputs")
			var err error
			row.Data, err = json.Marshal(data)
			if err != nil {
				return Payload{}, err
			}
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
