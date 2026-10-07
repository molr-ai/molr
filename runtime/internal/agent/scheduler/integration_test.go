package scheduler

import (
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

func TestTDDRetryKeepsLogicalExecutionButRejectsOldAcknowledgement(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/scheduler", newContractFixture)
	ctx := testkit.Context(t)
	f.SeedJob(scheduled(f, "retry-identity"))
	first, err := f.Queue.Claim(ctx, f.Now)
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, f.Queue.Retry(ctx, first.Principal, RetryRequest{JobID: first.Job.Event.ID, ExpectedVersion: first.Job.Version, NotBefore: f.Now.Add(time.Second), FailureCode: core.Unavailable}))
	second, err := f.Queue.Claim(ctx, f.Now.Add(time.Second))
	testkit.RequireNoError(t, err)
	if second.Job.ExecutionID != first.Job.ExecutionID || second.Job.Version <= first.Job.Version {
		t.Fatal("safe retry lost logical identity or version fencing")
	}
	testkit.RequireCode(t, f.Queue.Ack(ctx, first.Principal, first.Job.Event.ID, first.Job.Version), core.Conflict)
	testkit.RequireNoError(t, f.Queue.Ack(ctx, second.Principal, second.Job.Event.ID, second.Job.Version))
}
