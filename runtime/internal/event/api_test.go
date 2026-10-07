package event

import (
	"testing"
	"time"
)

func TestScheduledInputCannotMasqueradeAsUserMessage(t *testing.T) {
	base := AssistantEvent{ID: "event", OwnerID: "owner", AssistantID: "assistant", CreatedAt: time.Now()}
	valid := base
	valid.Kind = ScheduleDue
	valid.ScheduleID = "schedule"
	valid.OccurrenceAt = time.Now()
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	forged := valid
	forged.MessageID = "virtual-user-message"
	if forged.Validate() == nil {
		t.Fatal("scheduled event accepted a user message")
	}
	forged.Kind = UserMessage
	if forged.Validate() == nil {
		t.Fatal("scheduled source accepted as user message")
	}
	for _, kind := range []Kind{OperationCompleted, ResumeRequested, Kind("assistant_publication")} {
		forged := base
		forged.Kind = kind
		forged.MessageID = "message"
		if forged.Validate() == nil {
			t.Fatalf("invalid source accepted for %s", kind)
		}
	}
}
