package scheduler

import (
	"context"
	"errors"
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
