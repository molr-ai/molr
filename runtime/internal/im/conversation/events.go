package conversation

import (
	"context"
	"time"

	"molr.ai/runtime/internal/core"
)

// MessageCreated is the durable IM outbox event for both send paths. It carries
// no Loop instructions. The Agent scheduler resolves the recipient using the
// business assistant binding and ignores assistant-authored messages.
type MessageCreated struct {
	Sequence       uint64 `json:",omitempty"`
	ID             core.ID
	OwnerID        core.ID
	ConversationID core.ID
	MessageID      core.ID
	Sender         Sender
	SenderID       core.ID
	At             time.Time
}

// Outbox is transaction-bound. Acknowledge and creation of any downstream
// assistant input must commit together. Pending does not remove events.
type Outbox interface {
	Append(context.Context, MessageCreated) error
	Pending(context.Context, int) ([]MessageCreated, error)
	Acknowledge(context.Context, core.ID) error
}
