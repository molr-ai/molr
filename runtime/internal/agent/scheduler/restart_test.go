package scheduler

import (
	"context"
	"errors"
	"molr.ai/runtime/internal/business/planstate"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"testing"
	"time"

	"molr.ai/runtime/tests/testkit"
)

func TestSchedulerShutdownPreservesRunningJobForStartup(t *testing.T) {
	f := newContractFixture(t)
	f.SeedJob(scheduled(f, "interrupted-job"))
	done := make(chan error, 1)
	go func() { done <- f.Service.Run(t.Context()) }()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	testkit.RequireNoError(t, f.AwaitStarted(ctx))
	testkit.RequireNoError(t, f.Service.Shutdown(ctx))
	testkit.RequireNoError(t, <-done)
	page, err := f.Queue.List(ctx, f.Owner.OwnerID, Query{})
	testkit.RequireNoError(t, err)
	if len(page.Items) != 1 || page.Items[0].State != Running || page.Items[0].InterruptedAt == nil || page.Items[0].FailureCode != "" {
		t.Fatalf("shutdown made work terminal: %+v", page.Items)
	}
	var recovered int
	s := f.Service.(*service)
	s.options.RecoverInterrupted = func(ctx context.Context, j Job) (bool, error) {
		recovered++
		if j.Event.ID != "interrupted-job" {
			return false, errors.New("wrong job")
		}
		return true, nil
	}
	testkit.RequireNoError(t, s.Recover(ctx))
	if recovered != 1 {
		t.Fatal("startup missed interrupted job")
	}
}

func TestSchedulerExplicitCancelRemainsTerminal(t *testing.T) {
	f := newContractFixture(t)
	f.SeedJob(scheduled(f, "cancel-job"))
	done := make(chan error, 1)
	go func() { done <- f.Service.Run(t.Context()) }()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	testkit.RequireNoError(t, f.AwaitStarted(ctx))
	testkit.RequireNoError(t, f.Service.Cancel(ctx, f.Owner, "cancel-job"))
	testkit.RequireNoError(t, f.Service.Shutdown(ctx))
	testkit.RequireNoError(t, <-done)
	page, err := f.Queue.List(ctx, f.Owner.OwnerID, Query{})
	testkit.RequireNoError(t, err)
	if len(page.Items) != 1 || page.Items[0].State != Canceled || page.Items[0].InterruptedAt != nil {
		t.Fatalf("user cancel became recoverable: %+v", page.Items)
	}
}

func TestTransactionalPlanDeletionCancelsRunningHandler(t *testing.T) {
	f := newContractFixture(t)
	s := f.Service.(*service)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ref := core.Object{Kind: "todo", ID: "plan"}
	testkit.RequireNoError(t, s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "business.todos", "owner/plan", map[string]any{"ID": "plan", "OwnerID": "owner", "State": "active", "Version": 1}); err != nil {
			return err
		}
		return tx.Put(ctx, "business.tasks", "owner/task", map[string]any{"ID": "task", "OwnerID": "owner", "Plan": ref, "State": "running", "Version": 1})
	}))
	job := scheduled(f, "plan-job")
	job.Event.TaskID = "task"
	f.SeedJob(job)
	started := make(chan struct{})
	ended := make(chan struct{})
	s.options.Handle = func(ctx context.Context, _ Claim) error { close(started); <-ctx.Done(); close(ended); return ctx.Err() }
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("handler did not start")
	}
	rollback := errors.New("rollback")
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := planstate.Delete(ctx, tx.Store(), "owner", ref, 1); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	select {
	case <-ended:
		t.Fatal("rolled-back delete canceled execution")
	default:
	}
	testkit.RequireNoError(t, planstate.Delete(ctx, s.store, "owner", ref, 1))
	select {
	case <-ended:
	case <-ctx.Done():
		t.Fatal("running handler survived deletion")
	}
	testkit.RequireNoError(t, s.Shutdown(ctx))
	testkit.RequireNoError(t, <-done)
	testkit.RequireNoError(t, s.Recover(ctx))
	jobs, err := s.queue.List(ctx, "owner", Query{})
	testkit.RequireNoError(t, err)
	if len(jobs.Items) != 1 || jobs.Items[0].State != Canceled {
		t.Fatal("deleted execution recovered", jobs)
	}
}
