package event

import (
	"molr.ai/runtime/internal/core"
	"time"
)

// InterruptBoundary is saved atomically with a manual stop. Delayed inputs keep
// their original timestamp/sequence, so draining an outbox cannot make old work
// eligible again. Message sequence distinguishes same-clock-tick new input.
type InterruptBoundary struct {
	At              time.Time
	ConversationID  core.ID
	ThroughSequence uint64
}

func (b InterruptBoundary) Blocks(e AssistantEvent) bool {
	if b.At.IsZero() || e.Kind == UserStopped {
		return false
	}
	if e.Kind == UserMessage && e.MessageSequence > 0 && b.ConversationID != "" {
		return e.MessageSequence <= b.ThroughSequence
	}
	if !e.CreatedAt.After(b.At) {
		return true
	}
	// A delayed scheduler tick must not wake the loop for an occurrence that was
	// already due when the user stopped it. Future occurrences remain eligible.
	return e.Kind == ScheduleDue && !e.OccurrenceAt.IsZero() && !e.OccurrenceAt.After(b.At)
}
