package scheduler

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// RequireRunningExecution checks persisted running identity for a trusted caller.
// It preserves the raw job state; cancellation boundaries are enforced separately.
func RequireRunningExecution(ctx context.Context, store persistence.Store, p core.Principal) error {
	return requireExecution(ctx, store, p.OwnerID, p.SubjectID, p.ExecutionID, persistence.Eq("State", Running), func(job Job) bool { return job.State == Running })
}

// RequireHistoricalExecution checks persisted identity for settlement only, including
// finished jobs. It cannot authorize a new execution or replace operation checks.
func RequireHistoricalExecution(ctx context.Context, store persistence.Store, e core.Execution) error {
	return requireExecution(ctx, store, e.Principal.OwnerID, e.AssistantID, e.Principal.ExecutionID, persistence.Eq("Event.TaskID", e.TaskID), func(job Job) bool { return job.Event.TaskID == e.TaskID })
}

func requireExecution(ctx context.Context, store persistence.Store, owner, assistant, execution core.ID, constraint persistence.Filter, matches func(Job) bool) error {
	jobs, err := persistence.Select[Job](ctx, store, jobsBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("Event.OwnerID", owner), persistence.Eq("Event.AssistantID", assistant), persistence.Eq("ExecutionID", execution), constraint}, Limit: 1, Unordered: true})
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if job.Event.OwnerID != owner || job.Event.AssistantID != assistant || job.ExecutionID != execution {
			continue
		}
		if matches(job) {
			return nil
		}
	}
	return core.NewError(core.Forbidden, "object access denied")
}
