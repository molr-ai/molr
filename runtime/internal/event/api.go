// Package event defines persistent inputs consumed by independent assistant execution.
package event

import (
	"context"
	"encoding/json"
	"time"

	"molr.ai/runtime/internal/core"
)

// DeliveryMode defines when an event may join an assistant execution.
type DeliveryMode string

const (
	Interrupt DeliveryMode = "interrupt"
	AfterStep DeliveryMode = "after_step"
	WhenIdle  DeliveryMode = "when_idle"
)

type Kind string

const BrowserControlReturned Kind = "browser_control_returned"

const (
	Born                        Kind = "born"
	ContextCompressionRequested Kind = "context_compression_requested"
	ShellTaskFinished           Kind = "shell_task_finished"
	ShellTasksRunning           Kind = "shell_tasks_running"
	DreamDue                    Kind = "dream_due"
	UserMessage                 Kind = "user_message"
	ScheduleDue                 Kind = "schedule_due"
	OperationCompleted          Kind = "operation_completed"
	ResumeRequested             Kind = "resume_requested"
	UserStopped                 Kind = "user_stopped"
)

// Recovery links a new execution to interrupted work. It is internal input,
// never a user message or a grant. Source preserves the original stop boundary.
type Recovery struct {
	PreviousEventID, PreviousExecutionID core.ID
	SourceKind                           Kind
	SourceCreatedAt                      time.Time
	SourceMessageSequence                uint64
	Reason                               string
	InterruptedAt                        *time.Time `json:",omitempty"`
	// Step preserves the task's execution limit across recovery events.
	Step, Attempts int
}

// AssistantEvent references its actual source and is never a chat message.
// TaskID associates a scheduled occurrence or a resume event with its execution. ScheduleVersion belongs only to
// ScheduleDue; zero supports legacy inputs, new dispatchers provide the revision.
type AssistantEvent struct {
	Recovery     *Recovery `json:",omitempty"`
	DeliveryMode DeliveryMode
	// Manual compression bypasses the automatic threshold, never capacity checks.
	Manual  bool            `json:",omitempty"`
	Details json.RawMessage `json:",omitempty"`
	// MessageSequence carries the original conversation order across outbox delivery.
	MessageSequence uint64 `json:",omitempty"`
	// ExecutionID identifies an interrupted execution for a non-waking UserStopped event.
	ExecutionID     core.ID `json:",omitempty"`
	ID              core.ID
	OwnerID         core.ID
	AssistantID     core.ID
	Kind            Kind
	MessageID       core.ID
	ScheduleID      core.ID
	ScheduleVersion core.Version
	OccurrenceAt    time.Time
	OperationID     core.ID
	TaskID          core.ID
	CreatedAt       time.Time
}

// Delivery supplies compatibility for persisted events predating explicit modes.
func (e AssistantEvent) Delivery() DeliveryMode {
	if e.DeliveryMode != "" {
		return e.DeliveryMode
	}
	switch e.Kind {
	case UserStopped:
		return Interrupt
	case ShellTaskFinished, ShellTasksRunning, OperationCompleted, ContextCompressionRequested:
		return AfterStep
	default:
		return WhenIdle
	}
}
func (e AssistantEvent) Validate() error {
	mode := e.Delivery()
	if r := e.Recovery; r != nil {
		if e.Kind != ResumeRequested || r.PreviousEventID.Validate() != nil || r.PreviousExecutionID.Validate() != nil || r.SourceCreatedAt.IsZero() || r.SourceCreatedAt.After(e.CreatedAt) || r.SourceKind == "" || r.Step < 0 || r.Attempts < 1 || (r.Reason != "runtime_stopped" && r.Reason != "runtime_interrupted") || (r.InterruptedAt != nil && (r.InterruptedAt.IsZero() || r.InterruptedAt.After(e.CreatedAt))) {
			return core.Invalid("recovery", "invalid runtime recovery source")
		}
	}
	if mode != Interrupt && mode != AfterStep && mode != WhenIdle {
		return core.Invalid("delivery_mode", "unknown delivery mode")
	}
	if e.Manual && e.Kind != ContextCompressionRequested {
		return core.Invalid("manual", "only compression events can be manual")
	}
	if e.Kind == ContextCompressionRequested && mode != AfterStep {
		return core.Invalid("delivery_mode", "compression requires after_step")
	}
	if e.Kind == UserStopped && mode != Interrupt {
		return core.Invalid("delivery_mode", "stop requires interrupt")
	}
	if (e.Kind == Born || e.Kind == DreamDue || e.Kind == ScheduleDue || e.Kind == ResumeRequested || e.Kind == BrowserControlReturned) && mode != WhenIdle {
		return core.Invalid("delivery_mode", "independent executions require when_idle")
	}
	for _, id := range []core.ID{e.ID, e.OwnerID, e.AssistantID} {
		if err := id.Validate(); err != nil {
			return err
		}
	}
	if e.CreatedAt.IsZero() {
		return core.Invalid("created_at", "required")
	}
	if e.Kind != UserMessage && e.MessageSequence != 0 {
		return core.Invalid("message_sequence", "only user-message events carry a message sequence")
	}
	if e.Kind != UserStopped && e.ExecutionID != "" {
		return core.Invalid("execution_id", "only control events reference an interrupted execution")
	}
	if len(e.Details) > 256<<10 || (len(e.Details) > 0 && (!json.Valid(e.Details) || (e.Kind != ShellTaskFinished && e.Kind != ShellTasksRunning && e.Kind != BrowserControlReturned))) {
		return core.Invalid("details", "invalid shell event details")
	}
	valid := false
	switch e.Kind {
	case Born:
		valid = e.ScheduleVersion == 0 && e.MessageID == "" && e.ScheduleID == "" && e.OperationID == "" && e.OccurrenceAt.IsZero() && e.TaskID == ""
	case DreamDue:
		valid = e.OperationID.Validate() == nil && !e.OccurrenceAt.IsZero() && e.MessageID == "" && e.ScheduleID == "" && e.ScheduleVersion == 0 && e.TaskID == ""
	case UserMessage:
		valid = e.ScheduleVersion == 0 && e.MessageID.Validate() == nil && e.ScheduleID == "" && e.OperationID == "" && e.OccurrenceAt.IsZero() && e.TaskID == ""
	case ScheduleDue:
		valid = e.ScheduleID.Validate() == nil && !e.OccurrenceAt.IsZero() && e.MessageID == "" && e.OperationID == "" && (e.TaskID == "" || e.TaskID.Validate() == nil)
	case ShellTaskFinished, ShellTasksRunning:
		valid = e.ScheduleVersion == 0 && e.OperationID.Validate() == nil && e.MessageID == "" && e.ScheduleID == "" && e.OccurrenceAt.IsZero() && e.TaskID == ""
	case OperationCompleted:
		valid = e.ScheduleVersion == 0 && e.OperationID.Validate() == nil && e.MessageID == "" && e.ScheduleID == "" && e.OccurrenceAt.IsZero() && e.TaskID == ""
	case UserStopped:
		valid = e.ScheduleVersion == 0 && e.MessageID == "" && e.ScheduleID == "" && e.OperationID == "" && e.OccurrenceAt.IsZero() && (e.TaskID == "" || e.TaskID.Validate() == nil) && (e.ExecutionID == "" || e.ExecutionID.Validate() == nil)
	case ContextCompressionRequested, ResumeRequested, BrowserControlReturned:
		valid = e.ScheduleVersion == 0 && e.MessageID == "" && e.ScheduleID == "" && e.OperationID == "" && e.OccurrenceAt.IsZero() && (e.TaskID == "" || e.TaskID.Validate() == nil)
	}
	if !valid {
		return core.Invalid("event", "source does not match event kind")
	}
	return nil
}

// Writer is transaction-bound. Deduplicates by source event/occurrence identity.
type Writer interface {
	AppendAssistant(context.Context, AssistantEvent) error
}
