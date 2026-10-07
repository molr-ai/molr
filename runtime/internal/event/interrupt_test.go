package event

import (
	"testing"
	"time"
)

func TestInterruptBoundaryDistinguishesOldAndNewSources(t *testing.T) {
	now := time.Now()
	b := InterruptBoundary{At: now, ConversationID: "chat", ThroughSequence: 4}
	for _, tc := range []struct {
		name    string
		source  AssistantEvent
		blocked bool
	}{
		{"old message", AssistantEvent{Kind: UserMessage, MessageSequence: 4, CreatedAt: now.Add(time.Second)}, true},
		{"new message same clock tick", AssistantEvent{Kind: UserMessage, MessageSequence: 5, CreatedAt: now}, false},
		{"legacy message", AssistantEvent{Kind: UserMessage, CreatedAt: now.Add(-time.Second)}, true},
		{"delayed occurrence", AssistantEvent{Kind: ScheduleDue, CreatedAt: now.Add(time.Second), OccurrenceAt: now.Add(-time.Second)}, true},
		{"future occurrence", AssistantEvent{Kind: ScheduleDue, CreatedAt: now.Add(time.Second), OccurrenceAt: now.Add(time.Hour)}, false},
		{"old operation", AssistantEvent{Kind: OperationCompleted, CreatedAt: now}, true},
		{"new operation", AssistantEvent{Kind: OperationCompleted, CreatedAt: now.Add(time.Second)}, false},
		{"old resume", AssistantEvent{Kind: ResumeRequested, CreatedAt: now.Add(-time.Second)}, true},
		{"new resume", AssistantEvent{Kind: ResumeRequested, CreatedAt: now.Add(time.Second)}, false},
		{"control", AssistantEvent{Kind: UserStopped, CreatedAt: now}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := b.Blocks(tc.source); got != tc.blocked {
				t.Fatalf("blocked=%v", got)
			}
			if (InterruptBoundary{}).Blocks(tc.source) {
				t.Fatal("empty boundary blocks events")
			}
		})
	}
}
