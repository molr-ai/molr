package scheduler

import (
	"context"

	"molr.ai/runtime/internal/im/conversation"
)

// MessageConsumer bridges IM to Agent execution. It resolves the conversation's
// assistant through business state, enqueues only real user messages, and
// atomically acknowledges the IM outbox with the deduplicated assistant event.
// Assistant publications are acknowledged without creating another input event.
// This is a trusted background port, never a client-facing ingestion endpoint.
type MessageConsumer interface {
	AcceptMessage(context.Context, conversation.MessageCreated) error
}
