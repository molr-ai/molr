package event

import (
	"testing"
	"time"
)

func TestRecoveryRetainsOriginalStopBoundary(t *testing.T) {
	now := time.Now().UTC()
	r := Recovery{PreviousEventID: "old-event", PreviousExecutionID: "old-execution", SourceKind: UserMessage, SourceCreatedAt: now.Add(-time.Minute), SourceMessageSequence: 10, Reason: "runtime_interrupted", Attempts: 1}
	e := AssistantEvent{ID: "resume", OwnerID: "owner", AssistantID: "assistant", Kind: ResumeRequested, CreatedAt: now.Add(time.Minute), Recovery: &r}
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	boundary := InterruptBoundary{At: now, ConversationID: "chat", ThroughSequence: 10}
	if !boundary.Blocks(e) {
		t.Fatal("new recovery timestamp bypassed stop")
	}
	r.SourceMessageSequence = 11
	if boundary.Blocks(e) {
		t.Fatal("post-stop input incorrectly blocked")
	}
	e.Kind = UserMessage
	e.MessageID = "message"
	if e.Validate() == nil {
		t.Fatal("user event accepted runtime recovery metadata")
	}
}
