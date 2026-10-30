// Package policy separates user/object authorization, assistant grants and budget accounting.
package policy

import (
	"context"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/quota"
)

type Action string

const (
	Read    Action = "read"
	Write   Action = "write"
	Execute Action = "execute"
	Manage  Action = "manage"
)

type CheckRequest struct {
	// ConsumeOnce is a trusted effect-admission check, never a discovery or periodic check.
	ConsumeOnce bool
	OperationID core.ID
	Program     *ProgramInvocation
	// FullAccessProbe is only for capability discovery, never execution.
	FullAccessProbe bool
	FullAccess      bool
	Target          core.Object
	Action          Action
	ResourceID      core.ID
	// ModelID is required at model catalogue/dispatch entry points. Transport
	// and resource metadata checks remain bound to ResourceID.
	ModelID     core.ID
	TaskID      core.ID
	Destination string
}
type Decision struct {
	Allowed    bool
	ReasonCode string
}

// Require fails closed. It checks current actor validity, ownership, token
// scopes and live grants; shape validation alone is never authorization.
type Authorizer interface {
	Require(context.Context, core.Principal, CheckRequest) error
}
type GrantUse struct {
	OperationID core.ID
	ExecutionID core.ID
	UsedAt      time.Time
}
type Grant struct {
	Used        *GrantUse `json:",omitempty"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Constraints *Constraints          `json:",omitempty"`
	Temporary   *TemporaryConstraints `json:",omitempty"`
	ID          core.ID
	OwnerID     core.ID
	AssistantID core.ID
	ResourceID  core.ID
	Actions     []Action
	Scope       Scope
	ExpiresAt   *time.Time
	RevokedAt   *time.Time
	Version     core.Version
}
type Scope struct {
	Once            bool             `json:",omitempty"`
	FullAccessScope *FullAccessScope `json:",omitempty"`
	FullAccess      bool             `json:",omitempty"`
	// Model grants cover exactly one model belonging to ResourceID.
	ModelID      core.ID
	PathPrefixes []string
	ReadOnly     bool
}
type GrantRequest struct {
	Constraints *Constraints          `json:",omitempty"`
	Temporary   *TemporaryConstraints `json:",omitempty"`
	Mutation    core.Mutation
	AssistantID core.ID
	ResourceID  core.ID
	// Actions must be supported by the resource's current provider capabilities.
	Actions   []Action
	Scope     Scope
	ExpiresAt *time.Time
}
type UpdateGrantRequest struct {
	Constraints *Constraints          `json:",omitempty"`
	Temporary   *TemporaryConstraints `json:",omitempty"`
	Mutation    core.Mutation
	GrantID     core.ID
	Actions     []Action
	Scope       Scope
	ExpiresAt   *time.Time
}
type RevokeRequest struct {
	Mutation core.Mutation
	GrantID  core.ID
}
type Query struct {
	AssistantID core.ID
	ResourceID  core.ID
	Page        core.PageRequest
}
type Grants interface {
	UpdateGrant(context.Context, core.Principal, UpdateGrantRequest) (Grant, error)
	Grant(context.Context, core.Principal, GrantRequest) (Grant, error)
	List(context.Context, core.Principal, Query) (core.Page[Grant], error)
	Revoke(context.Context, core.Principal, RevokeRequest) error
}
type Reservation = quota.Reservation
type ReserveRequest = quota.ReserveRequest
type SettleRequest = quota.SettleRequest
type UsageQuery = quota.UsageQuery
type Usage = quota.Usage

// UsageReader exposes accounting queries without mutation capabilities.
type UsageReader interface {
	Usage(context.Context, core.Principal, UsageQuery) (Usage, error)
}

// Reserve is atomic under concurrent calls and idempotent by operation ID.
// MarkUnknown ends the local hold while preserving uncertain costs for late
// settlement. Release records proven no-charge outcomes. Native amounts are retained;
// budget comparisons use a single recorded exchange-rate snapshot.
type Budget interface {
	UsageReader
	Reserve(context.Context, ReserveRequest) (Reservation, error)
	Settle(context.Context, core.Execution, SettleRequest) (Reservation, error)
	MarkUnknown(context.Context, core.Execution, core.ID) error
	Release(context.Context, core.Execution, core.ID) error
}
type Service interface {
	Authorizer
	Grants
	Budget
}

// Repository runs inside the caller's transaction. Reserve must enforce limits
// against current persisted balances; callers cannot pass an authoritative limit.
type Repository interface {
	Grants(context.Context, core.ID, Query) (core.Page[Grant], error)
	InsertGrant(context.Context, Grant) error
	RevokeGrant(context.Context, core.ID, core.ID, core.Version, time.Time) error
	Reserve(context.Context, ReserveRequest) (Reservation, error)
	Settle(context.Context, core.ID, SettleRequest) (Reservation, error)
	SetReservationState(context.Context, core.ID, core.ID, string) error
	Usage(context.Context, core.ID, UsageQuery) (Usage, error)
}
