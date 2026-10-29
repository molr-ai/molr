package telegram

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"testing"
	"time"
)

func TestSummarySkipsDeliveryHistoryAndChecksIdentity(t *testing.T) {
	f := setup(t)
	b := Binding{ID: "summary-binding", OwnerID: f.owner.OwnerID, AssistantID: "assistant", ConversationID: "chat", State: "reconnecting"}
	must(t, f.db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, bindingsBucket, persistence.Key(b.OwnerID, b.ConversationID), b); err != nil {
			return err
		}
		// Valid query fields, deliberately invalid payload. Summary must never decode it.
		return tx.Put(ctx, deliveriesBucket, persistence.Key(b.OwnerID, "delivery"), map[string]any{"BindingID": b.ID, "OwnerID": b.OwnerID, "CreatedAt": time.Now().UTC(), "Attempts": "invalid"})
	}))
	value, err := f.s.Summary(t.Context(), f.owner, "assistant")
	must(t, err)
	if value.Channel != "telegram" || value.State != "reconnecting" {
		t.Fatalf("unexpected summary: %+v", value)
	}
	if _, err := f.s.Status(t.Context(), f.owner, "assistant"); err == nil {
		t.Fatal("fixture did not exercise delivery decoding")
	}
	stranger := f.owner
	stranger.OwnerID = "other"
	if _, err := f.s.Summary(t.Context(), stranger, "assistant"); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("identity accepted: %v", err)
	}
}
