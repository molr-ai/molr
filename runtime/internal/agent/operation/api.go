// Package operation tracks durable external effects and explicit unknown outcomes.
package operation

import (
	"context"
	"encoding/json"
	"time"

	"molr.ai/runtime/internal/core"
)

type State string

const (
	Prepared  State = "prepared"
	Running   State = "running"
	Succeeded State = "succeeded"
	Failed    State = "failed"
	Unknown   State = "unknown"
	Canceled  State = "canceled"
)

type Operation struct {
	ID             core.ID
	OwnerID        core.ID
	AssistantID    core.ID
	TaskID         core.ID
	ExecutionID    core.ID
	ResourceID     core.ID
	Action         string
	State          State
	IdempotencyKey string
	InputDigest    string
	ReceiptID      core.ID
	Result         json.RawMessage
	FailureCode    core.Code
	Version        core.Version
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
type BeginRequest struct {
	Mutation    core.Mutation
	Execution   core.Execution
	ResourceID  core.ID
	Action      string
	InputDigest string
}
type Completion struct {
	ID              core.ID
	ExpectedVersion core.Version
	State           State
	ReceiptID       core.ID
	SafeResult      json.RawMessage
	FailureCode     core.Code
}
type ReconcileRequest struct {
	ID              core.ID
	ExpectedVersion core.Version
}
type Query struct {
	AssistantID core.ID
	TaskID      core.ID
	States      []State
	Page        core.PageRequest
}

// Recorder persists intent before effects, binds callbacks to execution identity
// and rejects stale completions. Retry reuses identity and checks input digest.
type Recorder interface {
	Begin(context.Context, BeginRequest) (Operation, error)
	Complete(context.Context, core.Execution, Completion) (Operation, error)
	MarkUnknown(context.Context, core.Execution, core.ID) error
}

// Control exposes owner-facing observation and explicit cancel/reconcile commands.
// It cannot manufacture intent or provider completion records.
type Control interface {
	Get(context.Context, core.Principal, core.ID) (Operation, error)
	List(context.Context, core.Principal, Query) (core.Page[Operation], error)
	Cancel(context.Context, core.Principal, core.ID) (Operation, error)
	Reconcile(context.Context, core.Principal, ReconcileRequest) (Operation, error)
}
type Service interface {
	Recorder
	Control
}
type Repository interface {
	Get(context.Context, core.ID, core.ID) (Operation, error)
	FindByKey(context.Context, core.ID, string) (Operation, error)
	List(context.Context, core.ID, Query) (core.Page[Operation], error)
	Insert(context.Context, Operation) error
	Save(context.Context, Operation, core.Version) error
}
