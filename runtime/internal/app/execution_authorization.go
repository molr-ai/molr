package app

import (
	"context"
	"molr.ai/runtime/internal/business/planstate"
	"molr.ai/runtime/internal/business/task"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/platform/sandbox"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/shellsession"
)

func (s *shellService) watchAuthorization(ctx context.Context, e core.Execution, c platform.Command) (*sandbox.Authorization, error) {
	deadline := c.AuthorizationDeadline
	return executionAuthorization(s.store, e, func(ctx context.Context) error {
		next, err := s.authorizationDeadline(ctx, e, c)
		if err != nil {
			return err
		}
		if !platform.NativeSandbox() {
			for _, network := range c.NetworkAuthorizations {
				if _, err := s.networkAuthorization(ctx, e, network); err != nil {
					return err
				}
			}
		}
		if !next.Equal(deadline) {
			if scope := shellsession.ManagedFor(ctx, e); scope != nil && scope.AuthorizationExpiry != nil {
				if err = scope.AuthorizationExpiry(next); err != nil {
					return err
				}
			}
			deadline = next
		}
		return nil
	})
}
func executionAuthorization(store persistence.Store, e core.Execution, check func(context.Context) error) (*sandbox.Authorization, error) {
	// Subscribe before the first check. Bucket notifications also cover new grants
	// and resource replacement; record scopes keep unrelated actor activity quiet.
	keys := []persistence.SignalKey{
		persistence.RecordSignal("auth.accounts", string(e.Principal.OwnerID)),
		persistence.RecordSignal("business.assistants", persistence.Key(e.Principal.OwnerID, e.AssistantID)),
	}
	for _, bucket := range []string{"security.grants", "auth.groups", "business.resources", "business.bindings", "sandbox.connections", "sandbox.epochs"} {
		keys = append(keys, persistence.BucketSignal(bucket))
	}
	// A parent's hiring authorization can revoke a worker's borrowed grants.
	var worker struct{ EmployerID core.ID }
	if err := store.Read(context.Background(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Get(ctx, "business.assistants", persistence.Key(e.Principal.OwnerID, e.AssistantID), &worker)
	}); err != nil && !persistence.Missing(err) {
		return nil, err
	}
	if worker.EmployerID != "" {
		keys = append(keys, persistence.RecordSignal("business.assistants", persistence.Key(e.Principal.OwnerID, worker.EmployerID)))
	}
	if e.TaskID != "" {
		keys = append(keys, persistence.RecordSignal("business.tasks", persistence.Key(e.Principal.OwnerID, e.TaskID)))
		work, err := task.NewRepository(store).Get(context.Background(), e.Principal.OwnerID, e.TaskID)
		if err != nil {
			return nil, err
		}
		if work.Plan.ID != "" {
			keys = append(keys, persistence.RecordSignal(planstate.Bucket(work.Plan), persistence.Key(e.Principal.OwnerID, work.Plan.ID)))
		}
	}
	watch, err := store.Watch(keys...)
	if err != nil {
		return nil, err
	}
	return &sandbox.Authorization{Watch: watch, Check: func(ctx context.Context) (time.Time, error) {
		checkedAt := time.Now()
		if err := checkExecutionPlan(ctx, store, e); err != nil {
			return time.Time{}, err
		}
		if err := check(ctx); err != nil {
			return time.Time{}, err
		}
		grants, err := policy.ActiveGrants(ctx, store, e.Principal.OwnerID, policy.Query{AssistantID: e.AssistantID}, checkedAt)
		if err != nil {
			return time.Time{}, err
		}
		var next time.Time
		now := checkedAt
		consider := func(at time.Time) {
			if at.After(now) && (next.IsZero() || at.Before(next)) {
				next = at
			}
		}
		for _, g := range grants {
			if g.ExpiresAt != nil {
				consider(*g.ExpiresAt)
			}
			if g.Temporary != nil {
				consider(g.Temporary.Until)
			}
		}
		return next, nil
	}}, nil
}

// Managed processes may outlive their turn and even a completed task, but never
// a deleted source plan. Pausing a plan only prevents new task starts.
func checkExecutionPlan(ctx context.Context, store persistence.Store, e core.Execution) error {
	if e.TaskID == "" {
		return nil
	}
	work, err := task.NewRepository(store).Get(ctx, e.Principal.OwnerID, e.TaskID)
	if err != nil {
		return err
	}
	if work.AssistantID != e.AssistantID {
		return denied()
	}
	if work.Plan.ID == "" {
		return nil
	}
	state, err := planstate.Load(ctx, store, e.Principal.OwnerID, work.Plan)
	if err != nil {
		return err
	}
	return planstate.Visible(state)
}

// Completion notifications retain their source operation identity. A late
// notification from a deleted plan must not start another model turn.
func checkEventPlan(ctx context.Context, store persistence.Store, e event.AssistantEvent) error {
	taskID := e.TaskID
	if taskID == "" && e.OperationID != "" {
		op, err := persistence.Load[struct{ TaskID core.ID }](ctx, store, "agent.operations", persistence.Key(e.OwnerID, e.OperationID))
		if err != nil && !persistence.Missing(err) {
			return err
		}
		taskID = op.TaskID
	}
	if taskID == "" {
		return nil
	}
	err := checkExecutionPlan(ctx, store, core.Execution{Principal: core.Principal{OwnerID: e.OwnerID}, AssistantID: e.AssistantID, TaskID: taskID})
	if persistence.Missing(err) {
		return core.NewError(core.Canceled, "source plan is no longer available")
	}
	return err
}
