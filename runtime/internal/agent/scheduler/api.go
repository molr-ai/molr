// Package scheduler defines persistent single-server event scheduling and graceful recovery.
package scheduler

import (
	"context"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
)

type State string

const (
	Queued      State = "queued"
	Running     State = "running"
	Done        State = "done"
	Retry       State = "retry"
	Failed      State = "failed"
	NeedsReview State = "needs_review"
	Canceled    State = "canceled"
)

type Job struct {
	// JoinedTo binds expedited input to its existing turn until checkpoint commit.
	JoinedTo    core.ID `json:",omitempty"`
	Event       event.AssistantEvent
	State       State
	ExecutionID core.ID
	Attempts    int
	NotBefore   time.Time
	FailureCode core.Code
	Version     core.Version
}
type Claim struct {
	Job       Job
	Principal core.Principal
}
type Query struct {
	AssistantID core.ID
	States      []State
	Page        core.PageRequest
}
type RetryRequest struct {
	JobID           core.ID
	ExpectedVersion core.Version
	NotBefore       time.Time
	FailureCode     core.Code
}

// Lifecycle starts bounded workers and the timer; one active turn per assistant.
// Recover reconciles running operations before retrying and uses current grants.
// Shutdown stops claims and checkpoints work within the supplied deadline.
type Control interface {
	Jobs(context.Context, core.Principal, Query) (core.Page[Job], error)
	Cancel(context.Context, core.Principal, core.ID) error
}
type Service interface {
	Control
	Run(context.Context) error
	Shutdown(context.Context) error
	Recover(context.Context) error
}

// Queue is trusted-internal. Claim atomically binds an execution identity.
// Ack checks identity and version. Checkpoint + Ack belong to one transaction;
// failure must leave the job recoverable. It is a single-server queue, no
// distributed service or leases are implied by the claim contract. Safe retries
// retain the logical ExecutionID for operation/receipt idempotency; each attempt
// advances Version so an old acknowledgement cannot finish a newer attempt.
type Queue interface {
	Claim(context.Context, time.Time) (Claim, error)
	Ack(context.Context, core.Principal, core.ID, core.Version) error
	Retry(context.Context, core.Principal, RetryRequest) error
	List(context.Context, core.ID, Query) (core.Page[Job], error)
	Cancel(context.Context, core.ID, core.ID) error
}
