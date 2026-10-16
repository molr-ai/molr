// Package scheduler defines persistent single-server event scheduling and graceful recovery.
package scheduler

import (
	"context"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
)

type State = event.JobState

const (
	Queued      = event.Queued
	Running     = event.Running
	Done        = event.Done
	Retry       = event.Retry
	Failed      = event.Failed
	NeedsReview = event.NeedsReview
	Canceled    = event.Canceled
	Interrupted = event.Interrupted
)

type Job = event.Job

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
// Recover hands ordinary interrupted work to independent resume events when configured;
// specialized jobs reconcile their operations before retrying. Current grants apply.
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
