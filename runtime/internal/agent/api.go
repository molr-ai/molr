// Package agent owns independent assistant execution, distinct from conversation history.
package agent

import (
	"context"
	"encoding/json"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/policy"
)

type State string

const (
	Idle     State = "idle"
	Working  State = "working"
	Dreaming State = "dreaming"
	Waiting  State = "waiting"
	Paused   State = "paused"
	Stopped  State = "stopped"
)

type ModelRetryStatus struct {
	Attempt, MaxAttempts int
	NextAt               time.Time
	Reason               string
}
type Status struct {
	Compression *CompressionResult `json:",omitempty"`
	ModelRetry  *ModelRetryStatus  `json:",omitempty"`
	Context     *ContextStatus     `json:",omitempty"`
	// TriggerMessageID identifies the message that started the current working turn.
	TriggerMessageID           core.ID         `json:",omitempty"`
	DreamDeadline              *time.Time      `json:",omitempty"`
	DreamRunID                 core.ID         `json:",omitempty"`
	InterruptedThroughSequence uint64          `json:",omitempty"`
	Expedite                   *ExpediteStatus `json:",omitempty"`
	AssistantID                core.ID
	State                      State
	ExecutionID                core.ID
	ActiveTaskID               core.ID
	WaitingReason              string
	UpdatedAt                  time.Time
}

// Checkpoint is private execution state. Context may include tool/model data
// and must not be returned by public status/SSE APIs. No raw credentials allowed.
type Checkpoint struct {
	OwnerID             core.ID
	AssistantID         core.ID
	ExecutionID         core.ID
	FormatVersion       uint32
	Sequence            uint64
	Context             json.RawMessage
	PendingOperationIDs []core.ID
	LastEventID         core.ID
	SavedAt             time.Time
}
type Turn struct {
	Attempt    int
	Principal  core.Principal
	Event      event.AssistantEvent
	Checkpoint *Checkpoint
}
type Outcome struct {
	Status     Status
	Checkpoint Checkpoint
	NextWakeAt *time.Time
}

// Runtime handles one event under a trusted execution identity. The scheduler
// ensures no concurrent turns for the same assistant. Handle must not silently
// acknowledge an event whose checkpoint/effects have not been committed.
type Observer interface {
	Activities(context.Context, core.Principal, []core.ID) ([]Activity, error)
	Compress(context.Context, core.Principal, core.ID, CompressionRequest) (CompressionResult, error)
	Expedite(context.Context, core.Principal, core.ID, ExpediteRequest) (ExpediteStatus, error)
	MessageMetrics(context.Context, core.Principal, core.ID, core.ID) (TurnMetrics, error)
	Capabilities(context.Context, core.Principal, core.ID) (Capabilities, error)
	Status(context.Context, core.Principal, core.ID) (Status, error)
	Stop(context.Context, core.Principal, core.ID) error
}
type Runtime interface {
	Observer
	Handle(context.Context, Turn) (Outcome, error)
}

// ContextBuilder performs bounded, authorized reads through explicit tools;
// it must never inject entire raw chat history as implicit model context.
type ContextBuilder interface {
	Build(context.Context, Turn) ([]model.Input, error)
}
type Checkpoints interface {
	Load(context.Context, core.ID, core.ID) (Checkpoint, error)
	Save(context.Context, Checkpoint, uint64) error
	// SaveIfChanged returns false for an unchanged state after validating the expected version.
	SaveIfChanged(context.Context, Checkpoint, uint64) (bool, error)
}

// Dependencies are the minimum execution ports; none expose secrets or storage.
type Dependencies struct {
	Models model.Client
	Tools  tools.Executor
	Policy policy.Authorizer
	Budget policy.Budget
}
