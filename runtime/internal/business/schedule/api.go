// Package schedule owns personal schedules and assistant plans, separate from execution mechanics.
package schedule

import (
	"context"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/trigger"
)

type Kind string

const (
	Personal      Kind = "personal"
	AssistantPlan Kind = "assistant_plan"
)

// Scheduling semantics live in the shared trigger package; the public schedule
// DTO keeps its existing field names and validation entry point.
type MissedPolicy = trigger.MissedPolicy

const (
	Skip     = trigger.Skip
	Coalesce = trigger.Coalesce
	CatchUp  = trigger.CatchUp
)

type Trigger trigger.Rule

func (t Trigger) Validate() error { return trigger.Rule(t).Validate() }

type Schedule struct {
	ID           core.ID
	OwnerID      core.ID
	Kind         Kind
	AssistantID  core.ID
	Title        string
	Instructions string
	Trigger      Trigger
	EndAt        *time.Time
	Enabled      bool
	NextAt       *time.Time
	Version      core.Version
}
type Query struct {
	AssistantID core.ID
	Kind        Kind
	Page        core.PageRequest
}
type CreateRequest struct {
	Mutation     core.Mutation
	Kind         Kind
	AssistantID  core.ID
	Title        string
	Instructions string
	Trigger      Trigger
	EndAt        *time.Time
}
type UpdateRequest struct {
	CreateRequest
	ID      core.ID
	Enabled bool
}
type DeleteRequest struct {
	Mutation core.Mutation
	ID       core.ID
}

// Occurrence is persisted and unique by schedule/revision/due time. Personal
// schedules do not produce assistant events. Disabled/dismissed plans never run.
type Occurrence struct {
	ScheduleID      core.ID
	ScheduleVersion core.Version
	DueAt           time.Time
}
type Service interface {
	Create(context.Context, core.Principal, CreateRequest) (Schedule, error)
	Get(context.Context, core.Principal, core.ID) (Schedule, error)
	List(context.Context, core.Principal, Query) (core.Page[Schedule], error)
	Update(context.Context, core.Principal, UpdateRequest) (Schedule, error)
	Delete(context.Context, core.Principal, DeleteRequest) error
}
type Repository interface {
	Get(context.Context, core.ID, core.ID) (Schedule, error)
	List(context.Context, core.ID, Query) (core.Page[Schedule], error)
	Insert(context.Context, Schedule) error
	Save(context.Context, Schedule, core.Version) error
	Delete(context.Context, core.ID, core.ID, core.Version) error
	Due(context.Context, time.Time, int) ([]Schedule, error)
	RecordOccurrence(context.Context, core.ID, Occurrence) (bool, error)
}
