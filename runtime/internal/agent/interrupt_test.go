package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/tests/testkit"
)

func TestManualInterruptRecordsNonWakingEventAndAcceptsEveryTrigger(t *testing.T) {
	for _, kind := range []event.Kind{event.UserMessage, event.ScheduleDue, event.OperationCompleted, event.ResumeRequested} {
		t.Run(string(kind), func(t *testing.T) {
			f, db := makeLoopFixture(t, nil)
			e := f.Runtime.(*Engine)
			testkit.RequireNoError(t, e.Stop(t.Context(), f.Owner, "assistant"))
			status, err := e.Status(t.Context(), f.Owner, "assistant")
			testkit.RequireNoError(t, err)
			if status.State != Idle || len(f.ModelRequests()) != 0 {
				t.Fatal("interrupt disabled or woke the loop")
			}
			events, err := persistence.Scan[event.AssistantEvent](t.Context(), db, "agent.events")
			testkit.RequireNoError(t, err)
			if len(events) != 1 || events[0].Kind != event.UserStopped {
				t.Fatal("missing user stop event", events)
			}
			_, err = scheduler.NewQueue(db).Claim(t.Context(), time.Now().Add(time.Hour))
			testkit.RequireCode(t, err, core.NotFound)
			turn := f.Turn
			turn.Event = event.AssistantEvent{ID: "next", OwnerID: f.Owner.OwnerID, AssistantID: "assistant", Kind: kind, CreatedAt: time.Now()}
			switch kind {
			case event.UserMessage:
				turn.Event.MessageID = "history"
			case event.ScheduleDue:
				turn.Event.ScheduleID = "schedule"
				turn.Event.OccurrenceAt = time.Now()
			case event.OperationCompleted:
				turn.Event.OperationID = "completed"
			}
			_, err = e.Handle(t.Context(), turn)
			testkit.RequireNoError(t, err)
			requests := f.ModelRequests()
			want := 1
			if kind == event.UserMessage {
				want++ // one-time missing-publication reminder
			}
			if len(requests) != want {
				t.Fatal("future event was not handled")
			}
			raw, _ := json.Marshal(requests[0].Inputs)
			if !strings.Contains(string(raw), "user_stopped") || len(requests[0].Inputs) == 0 || requests[0].Inputs[0].Role != model.System || requests[0].Inputs[0].Text != loopInstructions {
				t.Fatal("control event missing from next context")
			}
		})
	}
}

func TestManualInterruptPreservesCompletedContextAndClosesPendingTools(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	response := model.Input{Role: model.Assistant, Calls: []model.ToolCall{{ID: "pending", Name: "conversation_publish", Arguments: json.RawMessage(`{"conversation_id":"chat","text":"not sent"}`)}}}
	progress := turnProgress{Inputs: []model.Input{{Role: model.System, Text: "retained work"}, response}, Response: &response}
	testkit.RequireNoError(t, e.saveProgress(t.Context(), f.Turn, &progress))
	testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, statusBucket, "owner/assistant", Status{AssistantID: "assistant", State: Working, ExecutionID: "run"}); err != nil {
			return err
		}
		return tx.Put(ctx, "agent.jobs", "owner/event", scheduler.Job{Event: f.Turn.Event, ExecutionID: "run", State: scheduler.Running, Version: 1})
	}))
	testkit.RequireNoError(t, e.Stop(t.Context(), f.Owner, "assistant"))
	cp, err := e.checkpoints.Load(t.Context(), "owner", "assistant")
	testkit.RequireNoError(t, err)
	var saved privateContext
	testkit.RequireNoError(t, json.Unmarshal(cp.Context, &saved))
	if len(saved.Inputs) != 4 || saved.Inputs[2].Role != model.Tool || saved.Inputs[2].ReplyTo != "pending" || !strings.Contains(saved.Inputs[2].Text, "not_started") || saved.Inputs[3].Role != model.User || !strings.Contains(saved.Inputs[3].Text, "user_stopped") {
		t.Fatal("incomplete interrupted checkpoint", string(cp.Context))
	}
	job, err := persistence.Load[scheduler.Job](t.Context(), db, "agent.jobs", "owner/event")
	testkit.RequireNoError(t, err)
	if job.State != scheduler.Canceled {
		t.Fatal("old execution still active")
	}
	turn := f.Turn
	turn.Event.ID = "next"
	turn.Event.CreatedAt = time.Now()
	turn.Principal.ExecutionID = "next-run"
	_, err = e.Handle(t.Context(), turn)
	testkit.RequireNoError(t, err)
}

func TestPersistentStopRemainsSeparateFromManualInterrupt(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	testkit.RequireNoError(t, e.Lifecycle(t.Context(), f.Owner, "assistant", "stop"))
	testkit.RequireCode(t, e.Stop(t.Context(), f.Owner, "assistant"), core.FailedPrecondition)
	_, err := e.Handle(t.Context(), f.Turn)
	testkit.RequireCode(t, err, core.FailedPrecondition)
	status, err := e.Status(t.Context(), f.Owner, "assistant")
	testkit.RequireNoError(t, err)
	if status.State != Stopped {
		t.Fatal("persistent stop was cleared")
	}
}

func TestManualStopFencesQueuedDelayedAndRestartedEvents(t *testing.T) {
	for _, kind := range []event.Kind{event.UserMessage, event.ScheduleDue, event.OperationCompleted, event.ResumeRequested} {
		t.Run(string(kind), func(t *testing.T) {
			f, db := makeLoopFixture(t, nil)
			e := f.Runtime.(*Engine)
			before := time.Now().Add(-time.Minute)
			source := func(id core.ID, at time.Time) event.AssistantEvent {
				v := event.AssistantEvent{ID: id, OwnerID: "owner", AssistantID: "assistant", Kind: kind, CreatedAt: at}
				switch kind {
				case event.UserMessage:
					v.MessageID = id
				case event.ScheduleDue:
					v.ScheduleID = id
					v.OccurrenceAt = at
				case event.OperationCompleted:
					v.OperationID = id
				}
				return v
			}
			writer := event.NewWriter(db)
			queued := source("queued-before", before)
			testkit.RequireNoError(t, writer.AppendAssistant(t.Context(), queued))
			testkit.RequireNoError(t, e.Stop(t.Context(), f.Owner, "assistant"))
			// This producer delivers an old source after the stop transaction.
			delayed := source("late-before", before)
			testkit.RequireNoError(t, writer.AppendAssistant(t.Context(), delayed))
			q := scheduler.NewQueue(db)
			_, err := q.Claim(t.Context(), time.Now())
			testkit.RequireCode(t, err, core.NotFound)
			page, err := q.List(t.Context(), "owner", scheduler.Query{})
			testkit.RequireNoError(t, err)
			if len(page.Items) != 2 {
				t.Fatal("old sources disappeared")
			}
			for _, job := range page.Items {
				if job.State != scheduler.Canceled {
					t.Fatal("old event remained eligible", job)
				}
			}
			turn := f.Turn
			turn.Event = delayed
			_, err = f.Restart().Handle(t.Context(), turn)
			testkit.RequireCode(t, err, core.Canceled)
			if len(f.ModelRequests()) != 0 {
				t.Fatal("old event reached model after restart")
			}
			fresh := source("after", time.Now())
			testkit.RequireNoError(t, writer.AppendAssistant(t.Context(), fresh))
			claim, err := q.Claim(t.Context(), time.Now())
			testkit.RequireNoError(t, err)
			if claim.Job.Event.ID != fresh.ID {
				t.Fatal("new event did not run")
			}
		})
	}
}

func TestManualStopFencesOutboxWithoutSuppressingSameTickNewMessage(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	now := time.Now()
	e.options.Now = func() time.Time { return now }
	chat := conversation.NewService(db, conversation.Options{Authorizer: fixtureAuthorizer{}, Now: func() time.Time { return now }})
	send := func(key string) conversation.Message {
		t.Helper()
		m, err := chat.SendUser(t.Context(), f.Owner, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: key}, ConversationID: "chat", Content: conversation.Content{Kind: conversation.Text, Text: key}})
		testkit.RequireNoError(t, err)
		return m
	}
	old := send("before-stop")
	testkit.RequireNoError(t, e.Stop(t.Context(), f.Owner, "assistant"))
	newer := send("after-stop")
	pending, err := conversation.NewOutbox(db).Pending(t.Context(), 100)
	testkit.RequireNoError(t, err)
	consumer := scheduler.NewMessageConsumer(db, scheduler.Options{})
	for _, input := range pending {
		testkit.RequireNoError(t, consumer.AcceptMessage(t.Context(), input))
	}
	q := scheduler.NewQueue(db)
	claim, err := q.Claim(t.Context(), now.Add(time.Second))
	testkit.RequireNoError(t, err)
	if claim.Job.Event.MessageID != newer.ID {
		t.Fatal("stale outbox input woke loop", claim.Job.Event)
	}
	page, err := q.List(t.Context(), "owner", scheduler.Query{})
	testkit.RequireNoError(t, err)
	for _, job := range page.Items {
		if job.Event.MessageID == old.ID && job.State != scheduler.Canceled {
			t.Fatal("old input not canceled")
		}
	}
	// Another owner's boundary and inputs remain independent.
	other := event.AssistantEvent{ID: "other-input", OwnerID: "other", AssistantID: "assistant", Kind: event.ResumeRequested, CreatedAt: now.Add(-time.Hour)}
	testkit.RequireNoError(t, event.NewWriter(db).AppendAssistant(t.Context(), other))
	claim, err = q.Claim(t.Context(), now.Add(time.Second))
	testkit.RequireNoError(t, err)
	if claim.Job.Event.OwnerID != "other" {
		t.Fatal("interrupt crossed owner boundary")
	}
}
