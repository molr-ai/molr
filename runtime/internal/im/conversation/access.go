package conversation

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"slices"
)

// Trusted read for ownership checks; callers authenticate and authorize the principal.
func AttachmentConversations(ctx context.Context, store persistence.Store, owner, file core.ID) ([]core.ID, error) {
	rows, err := persistence.Select[Message](ctx, store, "im.messages", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerKey", owner), {Field: "Content.AttachmentIDs", Op: "has", Value: string(file)}}, Fields: []string{"ConversationID", "Content.AttachmentIDs"}})
	if err != nil {
		return nil, err
	}
	out := []core.ID{}
	seen := map[core.ID]bool{}
	for _, m := range rows {
		if !seen[m.ConversationID] && slices.Contains(m.Content.AttachmentIDs, file) {
			out = append(out, m.ConversationID)
			seen[m.ConversationID] = true
		}
	}
	return out, nil
}

// LoadMessage reads message identity without applying publication or read-cursor effects.
func LoadMessage(ctx context.Context, store persistence.Store, owner, id core.ID) (Message, error) {
	return persistence.Load[Message](ctx, store, "im.messages", persistence.Key(owner, id))
}
