package event

import (
	"testing"
	"time"
)

func TestDeliveryDefaultsAndValidation(t *testing.T) {
	for _, tc := range []struct {
		kind Kind
		mode DeliveryMode
	}{{WatchFailed, WhenIdle}, {Born, WhenIdle}, {UserMessage, WhenIdle}, {ScheduleDue, WhenIdle}, {ResumeRequested, WhenIdle}, {DreamDue, WhenIdle}, {UserStopped, Interrupt}, {ShellTaskFinished, AfterStep}, {ShellTasksRunning, AfterStep}, {OperationCompleted, AfterStep}, {ContextCompressionRequested, AfterStep}} {
		if got := (AssistantEvent{Kind: tc.kind}).Delivery(); got != tc.mode {
			t.Fatalf("%s: %s", tc.kind, got)
		}
	}
	source := AssistantEvent{ID: "compression", OwnerID: "owner", AssistantID: "assistant", Kind: ContextCompressionRequested, DeliveryMode: AfterStep, Manual: true, CreatedAt: time.Now()}
	if err := source.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []DeliveryMode{Interrupt, WhenIdle, "unknown"} {
		source.DeliveryMode = mode
		if source.Validate() == nil {
			t.Fatalf("accepted compression mode %s", mode)
		}
	}
}
