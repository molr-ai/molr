package agent

import (
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
)

func TestTurnRejectsCrossOwnerAndAssistantCheckpoints(t *testing.T) {
	turn := Turn{Principal: core.Principal{OwnerID: "o", SubjectID: "a", Kind: core.ActorAssistant, ExecutionID: "run"}, Event: event.AssistantEvent{ID: "event", OwnerID: "o", AssistantID: "a", Kind: event.UserMessage, MessageID: "message", CreatedAt: time.Now()}}
	if err := turn.Validate(); err != nil {
		t.Fatal(err)
	}
	turn.Event.OwnerID = "other"
	if turn.Validate() == nil {
		t.Fatal("cross-owner input accepted")
	}
	turn.Event.OwnerID = "o"
	turn.Checkpoint = &Checkpoint{OwnerID: "o", AssistantID: "other", FormatVersion: 1}
	if turn.Validate() == nil {
		t.Fatal("cross-assistant checkpoint accepted")
	}
}
