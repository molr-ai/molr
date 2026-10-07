package scheduler

import (
	"context"
	"reflect"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/tests/testkit"
)

// SeedJob installs storage records (not Queue responses), preserving requested
// state/version. IM messages are bound to AssistantID through real business data.
// The execution dependency is deterministic and held at a checkpoint until
// ReleaseTurns; counters observe calls to that dependency, not fake Service output.
type contractFixture struct {
	Service               Service
	Queue                 Queue
	Consumer              MessageConsumer
	Outbox                conversation.Outbox
	Owner, Other          core.Principal
	AssistantID           core.ID
	ConversationID        core.ID
	Now                   time.Time
	SeedJob               func(Job)
	AssistantEvents       func() []event.AssistantEvent
	FailNextCommit        func()
	AwaitStarted          func(context.Context) error
	ReleaseTurns          func()
	AwaitCompleted        func(context.Context, int) error
	MaxActiveForAssistant func(core.ID) int
	ExternalEffects       func() int
}

var newContractFixture func(*testing.T) contractFixture

func scheduled(f contractFixture, id core.ID) Job {
	return Job{Event: event.AssistantEvent{ID: id, OwnerID: f.Owner.OwnerID, AssistantID: f.AssistantID, Kind: event.ScheduleDue, ScheduleID: "schedule", OccurrenceAt: f.Now, CreatedAt: f.Now}, State: Queued, NotBefore: f.Now, Version: 1}
}
func TestTDDQueueClaimSerializesAssistantAndChecksAckIdentity(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/scheduler", newContractFixture)
	ctx := testkit.Context(t)
	f.SeedJob(scheduled(f, "job-1"))
	second := scheduled(f, "job-2")
	second.Event.OccurrenceAt = second.Event.OccurrenceAt.Add(time.Minute)
	f.SeedJob(second)
	claim, err := f.Queue.Claim(ctx, f.Now)
	testkit.RequireNoError(t, err)
	if claim.Job.State != Running || claim.Job.ExecutionID == "" || claim.Principal.ExecutionID != claim.Job.ExecutionID || claim.Principal.SubjectID != f.AssistantID {
		t.Fatalf("claim identity = %+v", claim)
	}
	_, err = f.Queue.Claim(ctx, f.Now)
	if err == nil {
		t.Fatal("two turns claimed for the same assistant")
	}
	foreign := claim.Principal
	foreign.ExecutionID = "old-execution"
	err = f.Queue.Ack(ctx, foreign, claim.Job.Event.ID, claim.Job.Version)
	if err == nil {
		t.Fatal("stale execution acknowledged a job")
	}
	testkit.RequireCode(t, f.Queue.Ack(ctx, claim.Principal, claim.Job.Event.ID, claim.Job.Version+1), core.Conflict)
	testkit.RequireNoError(t, f.Queue.Ack(ctx, claim.Principal, claim.Job.Event.ID, claim.Job.Version))
	jobs, err := f.Queue.List(ctx, f.Owner.OwnerID, Query{States: []State{Done}})
	testkit.RequireNoError(t, err)
	if len(jobs.Items) != 1 || jobs.Items[0].Event.ID != claim.Job.Event.ID {
		t.Fatalf("ack not durable: %+v", jobs)
	}
	next, err := f.Queue.Claim(ctx, f.Now)
	testkit.RequireNoError(t, err)
	if next.Job.Event.ID == claim.Job.Event.ID {
		t.Fatal("acknowledged job claimed again")
	}
}
func TestTDDQueueRetryHonorsDelayAndCancelPreventsClaim(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/scheduler", newContractFixture)
	ctx := testkit.Context(t)
	f.SeedJob(scheduled(f, "retry-job"))
	claim, err := f.Queue.Claim(ctx, f.Now)
	testkit.RequireNoError(t, err)
	req := RetryRequest{JobID: claim.Job.Event.ID, ExpectedVersion: claim.Job.Version, NotBefore: f.Now.Add(time.Hour), FailureCode: core.Unavailable}
	testkit.RequireNoError(t, f.Queue.Retry(ctx, claim.Principal, req))
	_, err = f.Queue.Claim(ctx, f.Now)
	if err == nil {
		t.Fatal("retry ran before NotBefore")
	}
	claim, err = f.Queue.Claim(ctx, req.NotBefore)
	testkit.RequireNoError(t, err)
	if claim.Job.Attempts < 2 {
		t.Fatalf("attempt count not advanced: %+v", claim.Job)
	}
	testkit.RequireNoError(t, f.Queue.Cancel(ctx, f.Owner.OwnerID, claim.Job.Event.ID))
	jobs, err := f.Queue.List(ctx, f.Owner.OwnerID, Query{States: []State{Canceled}})
	testkit.RequireNoError(t, err)
	if len(jobs.Items) != 1 {
		t.Fatalf("canceled jobs = %+v", jobs)
	}
	_, err = f.Queue.Claim(ctx, req.NotBefore.Add(time.Hour))
	if err == nil {
		t.Fatal("canceled job claimed")
	}
}
func TestTDDSchedulerControlIsOwnerScoped(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/scheduler", newContractFixture)
	ctx := testkit.Context(t)
	f.SeedJob(scheduled(f, "owned-job"))
	page, err := f.Service.Jobs(ctx, f.Owner, Query{AssistantID: f.AssistantID})
	testkit.RequireNoError(t, err)
	if len(page.Items) != 1 {
		t.Fatalf("jobs = %+v", page)
	}
	err = f.Service.Cancel(ctx, f.Other, "owned-job")
	if c := core.CodeOf(err); c != core.Forbidden && c != core.NotFound {
		t.Fatalf("foreign cancellation = %v", err)
	}
	foreign, err := f.Service.Jobs(ctx, f.Other, Query{})
	testkit.RequireNoError(t, err)
	if len(foreign.Items) != 0 {
		t.Fatal("cross-owner job leak")
	}
	testkit.RequireNoError(t, f.Service.Cancel(ctx, f.Owner, "owned-job"))
	page, err = f.Service.Jobs(ctx, f.Owner, Query{States: []State{Canceled}})
	testkit.RequireNoError(t, err)
	if len(page.Items) != 1 {
		t.Fatal("cancel was not persisted")
	}
}
func TestTDDSchedulerRunShutdownMaintainsSingleActiveTurn(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/scheduler", newContractFixture)
	ctx := testkit.Context(t)
	f.SeedJob(scheduled(f, "run-1"))
	second := scheduled(f, "run-2")
	second.Event.OccurrenceAt = second.Event.OccurrenceAt.Add(time.Second)
	f.SeedJob(second)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.Service.Run(runCtx) }()
	testkit.RequireNoError(t, f.AwaitStarted(ctx))
	f.ReleaseTurns()
	testkit.RequireNoError(t, f.AwaitCompleted(ctx, 2))
	testkit.RequireNoError(t, f.Service.Shutdown(ctx))
	select {
	case err := <-done:
		if err != nil && core.CodeOf(err) != core.Canceled {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("Run did not stop after Shutdown")
	}
	if f.MaxActiveForAssistant(f.AssistantID) != 1 {
		t.Fatalf("max active = %d", f.MaxActiveForAssistant(f.AssistantID))
	}
	jobs, err := f.Service.Jobs(ctx, f.Owner, Query{States: []State{Done}})
	testkit.RequireNoError(t, err)
	if len(jobs.Items) != 2 {
		t.Fatalf("completed jobs = %+v", jobs)
	}
}
func TestTDDSchedulerRecoveryDoesNotBlindlyReplayUnknownEffect(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/scheduler", newContractFixture)
	ctx := testkit.Context(t)
	// Adapter seeds an unresolved external operation for each running job.
	job := scheduled(f, "interrupted")
	job.State = Running
	job.ExecutionID = "interrupted-run"
	job.Attempts = 1
	f.SeedJob(job)
	before := f.ExternalEffects()
	testkit.RequireNoError(t, f.Service.Recover(ctx))
	testkit.RequireNoError(t, f.Service.Recover(ctx))
	if f.ExternalEffects() != before {
		t.Fatal("recovery repeated unknown external effect")
	}
	jobs, err := f.Service.Jobs(ctx, f.Owner, Query{})
	testkit.RequireNoError(t, err)
	if len(jobs.Items) != 1 || jobs.Items[0].State != NeedsReview {
		t.Fatalf("unresolved effect not held for review: %+v", jobs)
	}
}
func TestTDDMessageConsumerWakesOnlyRealUserAndDeduplicates(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/scheduler", newContractFixture)
	ctx := testkit.Context(t)
	user := conversation.MessageCreated{ID: "im-user", OwnerID: f.Owner.OwnerID, ConversationID: f.ConversationID, MessageID: "user-message", Sender: conversation.User, SenderID: f.Owner.SubjectID, At: f.Now}
	testkit.RequireNoError(t, f.Outbox.Append(ctx, user))
	testkit.RequireNoError(t, f.Consumer.AcceptMessage(ctx, user))
	testkit.RequireNoError(t, f.Consumer.AcceptMessage(ctx, user))
	reply := user
	reply.ID = "im-reply"
	reply.MessageID = "assistant-message"
	reply.Sender = conversation.Assistant
	reply.SenderID = f.AssistantID
	testkit.RequireNoError(t, f.Outbox.Append(ctx, reply))
	testkit.RequireNoError(t, f.Consumer.AcceptMessage(ctx, reply))
	events := f.AssistantEvents()
	if len(events) != 1 || events[0].Kind != event.UserMessage || events[0].MessageID != user.MessageID || events[0].AssistantID != f.AssistantID {
		t.Fatalf("wrong loop inputs: %+v", events)
	}
	pending, err := f.Outbox.Pending(ctx, 10)
	testkit.RequireNoError(t, err)
	if len(pending) != 0 {
		t.Fatalf("consumed IM events not acknowledged: %+v", pending)
	}
}
func TestTDDMessageConsumerFailureRollsBackInputAndAcknowledgement(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/scheduler", newContractFixture)
	ctx := testkit.Context(t)
	e := conversation.MessageCreated{ID: "im-failed", OwnerID: f.Owner.OwnerID, ConversationID: f.ConversationID, MessageID: "message", Sender: conversation.User, SenderID: f.Owner.SubjectID, At: f.Now}
	testkit.RequireNoError(t, f.Outbox.Append(ctx, e))
	f.FailNextCommit()
	if err := f.Consumer.AcceptMessage(ctx, e); err == nil {
		t.Fatal("commit failure ignored")
	}
	pending, err := f.Outbox.Pending(ctx, 10)
	testkit.RequireNoError(t, err)
	if len(pending) != 1 || len(f.AssistantEvents()) != 0 {
		t.Fatal("input append and IM acknowledgement did not roll back together")
	}
	testkit.RequireNoError(t, f.Consumer.AcceptMessage(ctx, e))
	if len(f.AssistantEvents()) != 1 {
		t.Fatal("failed message was not recoverable")
	}
}

func TestUserMessageEventStableIdentity(t *testing.T) {
	m := conversation.Message{ID: "message", CreatedAt: time.Now()}
	first := UserMessageEvent("owner", "assistant", m)
	if !reflect.DeepEqual(first, UserMessageEvent("owner", "assistant", m)) || first.ID == UserMessageEvent("other", "assistant", m).ID || first.ID == UserMessageEvent("owner", "other", m).ID {
		t.Fatal("message event identity is not scoped and stable")
	}
	if first.MessageID != m.ID || first.Kind != event.UserMessage || first.CreatedAt != m.CreatedAt {
		t.Fatal("message source changed")
	}
}

func TestRetryHoldsOnlyItsAssistantUntilDue(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/scheduler", newContractFixture)
	ctx := testkit.Context(t)
	f.SeedJob(scheduled(f, "first"))
	claim, err := f.Queue.Claim(ctx, f.Now)
	testkit.RequireNoError(t, err)
	due := f.Now.Add(time.Minute)
	testkit.RequireNoError(t, f.Queue.Retry(ctx, claim.Principal, RetryRequest{JobID: claim.Job.Event.ID, ExpectedVersion: claim.Job.Version, NotBefore: due}))
	f.SeedJob(scheduled(f, "later"))
	other := scheduled(f, "other")
	other.Event.AssistantID = "other-assistant"
	f.SeedJob(other)
	next, err := f.Queue.Claim(ctx, f.Now)
	testkit.RequireNoError(t, err)
	if next.Job.Event.ID != "other" {
		t.Fatal("later message overtook retry", next)
	}
	testkit.RequireNoError(t, f.Queue.Ack(ctx, next.Principal, next.Job.Event.ID, next.Job.Version))
	_, err = f.Queue.Claim(ctx, f.Now)
	testkit.RequireCode(t, err, core.NotFound)
	next, err = f.Queue.Claim(ctx, due)
	testkit.RequireNoError(t, err)
	if next.Job.Event.ID != "first" {
		t.Fatal("did not resume same turn", next)
	}
}
