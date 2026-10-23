// Package plan coordinates personal arrangements and assistant executions.
// Existing todo/schedule identities remain the source of truth for arrangements.
package plan

import (
	"context"
	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/core"
	"time"
)

type Execution struct {
	ID           core.ID
	State        string
	Title        string
	Instructions string
	PlanVersion  core.Version
	OccurrenceAt *time.Time
	StartedAt    time.Time
	CompletedAt  *time.Time
	Result       string
	Task         *task.Task
}
type Links struct {
	Ref          core.Object
	OwnerID      core.ID
	Dependencies []core.Object
	Handoff      string // notify (default) or automatic; explicitly configured by owner
	Paused       bool
	Version      core.Version
	Runs         []Execution
	Dispatched   bool
}
type Plan struct {
	Ref         core.Object
	Title       string
	AssistantID core.ID
	Todo        *todo.Todo
	Schedule    *schedule.Schedule
	Links       Links
	Executions  []Execution
	Occurrences []time.Time
	BlockedBy   []core.Object
	Ready       bool
}
type ConfigureRequest struct {
	Mutation     core.Mutation
	Ref          core.Object
	Dependencies []core.Object
	Handoff      string
	Paused       bool
}
type ExecuteRequest struct {
	Mutation     core.Mutation
	Ref          core.Object
	Action       string // start or complete for a personal occurrence
	OccurrenceAt *time.Time
	Result       string
}
type Service interface {
	Resolve(context.Context, core.Principal, core.ID) (core.Object, error)
	Overview(context.Context, core.Principal, time.Time, time.Time) ([]Summary, error)
	Detail(context.Context, core.Principal, core.Object, *time.Time) (Summary, error)
	History(context.Context, core.Principal, core.Object, core.PageRequest) (core.Page[ExecutionSummary], error)
	Execution(context.Context, core.Principal, core.Object, core.ID) (Execution, error)
	Events(context.Context, core.Principal, EventQuery) (core.Page[Event], error)
	List(context.Context, core.Principal, time.Time, time.Time) ([]Plan, error)
	Configure(context.Context, core.Principal, ConfigureRequest) (Links, error)
	Execute(context.Context, core.Principal, ExecuteRequest) (Execution, error)
}
