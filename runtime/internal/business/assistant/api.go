// Package assistant owns assistant identity and lifecycle independently of execution state.
package assistant

import (
	"context"
	"time"

	"molr.ai/runtime/internal/core"
)

type State string

const (
	Active    State = "active"
	Dismissed State = "dismissed"
)

type Assistant struct {
	ID             core.ID
	OwnerID        core.ID
	Name           string
	Avatar         string // Empty, preset:<id>, or a bounded PNG/JPEG data URL.
	State          State
	ConversationID core.ID
	GroupID        core.ID
	Pinned         bool
	ModelID        core.ID
	Budget         core.AssistantBudget
	Version        core.Version
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
type Group struct {
	ID       core.ID
	OwnerID  core.ID
	Name     string
	Position int
	Version  core.Version
}
type Query struct {
	State   State
	GroupID core.ID
	Name    string
	Page    core.PageRequest
}
type HireRequest struct {
	Avatar   string
	Mutation core.Mutation
	Name     string
	ModelID  core.ID
	Budget   core.AssistantBudget
}
type UpdateRequest struct {
	Avatar   *string // Omitted/null preserves the avatar; empty clears it.
	Mutation core.Mutation
	ID       core.ID
	Name     string
	ModelID  core.ID
	Budget   core.AssistantBudget
}
type PlacementRequest struct {
	Mutation core.Mutation
	ID       core.ID
	GroupID  core.ID
	Pinned   bool
}
type LifecycleRequest struct {
	Mutation core.Mutation
	ID       core.ID
}

// Rehire restores only the explicitly confirmed permissions and schedules.
type RehireRequest struct {
	LifecycleRequest
	RestoreGrantIDs    []core.ID
	RestoreScheduleIDs []core.ID
}

// Clone copies only requested history, memory and still-valid grant references;
// it must allocate a new identity, conversation and Loop, never running work.
type CloneRequest struct {
	Mutation     core.Mutation
	SourceID     core.ID
	Name         string
	CopyMessages bool
	CopyMemory   bool
	GrantIDs     []core.ID
}
type GroupRequest struct {
	Mutation core.Mutation
	ID       core.ID
	Name     string
	Position int
}

// Service checks current ownership and policy on every call. Hire creates the
// assistant, persistent conversation, selected model's read/execute grant
// and initial Loop state atomically. Model calls use the provider's current endpoint.
type Service interface {
	Hire(context.Context, core.Principal, HireRequest) (Assistant, error)
	Get(context.Context, core.Principal, core.ID) (Assistant, error)
	List(context.Context, core.Principal, Query) (core.Page[Assistant], error)
	Update(context.Context, core.Principal, UpdateRequest) (Assistant, error)
	UpdateSelfProfile(context.Context, core.Execution, SelfProfileRequest) (ProfileResult, error)
	Place(context.Context, core.Principal, PlacementRequest) (Assistant, error)
	Dismiss(context.Context, core.Principal, LifecycleRequest) (Assistant, error)
	Rehire(context.Context, core.Principal, RehireRequest) (Assistant, error)
	Clone(context.Context, core.Principal, CloneRequest) (Assistant, error)
	Groups(context.Context, core.Principal, core.PageRequest) (core.Page[Group], error)
	CreateGroup(context.Context, core.Principal, GroupRequest) (Group, error)
	UpdateGroup(context.Context, core.Principal, GroupRequest) (Group, error)
	DeleteGroup(context.Context, core.Principal, LifecycleRequest) error
}

// Repository is trusted-internal and transaction-bound. Owner IDs are never
// taken directly from an HTTP payload. Save uses optimistic version matching.
type Repository interface {
	Get(context.Context, core.ID, core.ID) (Assistant, error)
	List(context.Context, core.ID, Query) (core.Page[Assistant], error)
	Insert(context.Context, Assistant) error
	Save(context.Context, Assistant, core.Version) error
	Groups(context.Context, core.ID, core.PageRequest) (core.Page[Group], error)
	PutGroup(context.Context, Group, core.Version) error
	DeleteGroup(context.Context, core.ID, core.ID, core.Version) error
}
