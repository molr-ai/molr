package agent

import (
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
)

// A checkpoint summary of publication since the latest accepted user event.
// Runtime notices have the user role too, but cannot reset this boundary.
type publishReminderState struct {
	UserEventID     core.ID
	MessageSequence uint64
	Attempted       bool
	Reminded        bool
}

func (s *publishReminderState) accept(e event.AssistantEvent) {
	if e.Kind != event.UserMessage || e.ID == s.UserEventID {
		return
	}
	// Urgent intake can overtake older queued user events.
	if s.MessageSequence != 0 && e.MessageSequence != 0 && e.MessageSequence <= s.MessageSequence {
		return
	}
	*s = publishReminderState{UserEventID: e.ID, MessageSequence: e.MessageSequence}
}
