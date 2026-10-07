// Package task owns goals, revisions, user controls and verified delivery.
package task

import (
	"context"
	"time"

	"molr.ai/runtime/internal/core"
)

type State string

const (
	Pending   State = "pending"
	Running   State = "running"
	Waiting   State = "waiting"
	Paused    State = "paused"
	Completed State = "completed"
	Failed    State = "failed"
	Canceled  State = "canceled"
)

type Task struct {
	Plan             core.Object
	PlanTitle        string
	PlanInstructions string
	PlanVersion      core.Version
	OccurrenceAt     *time.Time
	ID               core.ID
	OwnerID          core.ID
	AssistantID      core.ID
	Goal             string
	State            State
	Revision         uint64

	Spent         core.Money
	ArtifactIDs   []core.ID
	WaitingReason string
	Version       core.Version
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
type Revision struct {
	Number             uint64
	Instructions       string
	AcceptanceCriteria []string
	ArtifactIDs        []core.ID
	CreatedAt          time.Time
}
type Query struct {
	AssistantID core.ID
	States      []State
	Page        core.PageRequest
}
type CreateRequest struct {
	Plan         core.Object
	OccurrenceAt *time.Time
	Mutation     core.Mutation
	AssistantID  core.ID
	Goal         string
	ResourceIDs  []core.ID

	AcceptanceCriteria []string
}
type ReviseRequest struct {
	Mutation           core.Mutation
	ID                 core.ID
	Instructions       string
	AcceptanceCriteria []string
}
type ControlRequest struct {
	Mutation core.Mutation
	ID       core.ID
}

// Delivery records actual verification evidence; model claims alone cannot
// mark a task completed. Revisions preserve previous outputs and total spend.
type Delivery struct {
	Mutation    core.Mutation
	TaskID      core.ID
	Revision    uint64
	ArtifactIDs []core.ID
	Checks      []Check
}
type Check struct {
	Criterion   string
	Passed      bool
	EvidenceIDs []core.ID
}
type Progress struct {
	TaskID          core.ID
	State           State
	Reason          string
	ExpectedVersion core.Version
}
type Service interface {
	Create(context.Context, core.Principal, CreateRequest) (Task, error)
	Get(context.Context, core.Principal, core.ID) (Task, error)
	List(context.Context, core.Principal, Query) (core.Page[Task], error)
	Revise(context.Context, core.Principal, ReviseRequest) (Task, error)
	Revisions(context.Context, core.Principal, core.ID, core.PageRequest) (core.Page[Revision], error)
	Pause(context.Context, core.Principal, ControlRequest) (Task, error)
	Resume(context.Context, core.Principal, ControlRequest) (Task, error)
	Cancel(context.Context, core.Principal, ControlRequest) (Task, error)
}

// Execution is an internal capability and is not exposed as a user status setter.
type Execution interface {
	Report(context.Context, core.Execution, Progress) (Task, error)
	Deliver(context.Context, core.Execution, Delivery) (Task, error)
}
type Repository interface {
	Get(context.Context, core.ID, core.ID) (Task, error)
	List(context.Context, core.ID, Query) (core.Page[Task], error)
	Insert(context.Context, Task) error
	Save(context.Context, Task, core.Version) error
	AppendRevision(context.Context, core.ID, core.ID, Revision) error
	Revisions(context.Context, core.ID, core.ID, core.PageRequest) (core.Page[Revision], error)
}
