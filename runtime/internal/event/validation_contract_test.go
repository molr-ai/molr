package event

import (
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

func TestTDDAssistantEventValidatesEverySourceAndRequiredIdentity(t *testing.T) {
	now := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name                         string
		kind                         Kind
		message, schedule, operation core.ID
		occurrence                   time.Time
	}{
		{name: "user", kind: UserMessage, message: "message"},
		{name: "schedule", kind: ScheduleDue, schedule: "schedule", occurrence: now},
		{name: "operation", kind: OperationCompleted, operation: "operation"},
		{name: "resume", kind: ResumeRequested},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := AssistantEvent{ID: "event", OwnerID: "owner", AssistantID: "assistant", Kind: tc.kind, MessageID: tc.message, ScheduleID: tc.schedule, OperationID: tc.operation, OccurrenceAt: tc.occurrence, CreatedAt: now}
			testkit.RequireNoError(t, e.Validate())
			missing := e
			missing.CreatedAt = time.Time{}
			testkit.RequireCode(t, missing.Validate(), core.InvalidArgument)
			for _, mutate := range []func(*AssistantEvent){func(x *AssistantEvent) { x.ID = "" }, func(x *AssistantEvent) { x.OwnerID = "" }, func(x *AssistantEvent) { x.AssistantID = "../bad" }} {
				bad := e
				mutate(&bad)
				testkit.RequireCode(t, bad.Validate(), core.InvalidArgument)
			}
		})
	}
}
