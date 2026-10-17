package agent

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/prompt"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/tests/testkit"
)

func reminderCount(inputs []model.Input) int {
	n := 0
	for _, in := range inputs {
		if in.Role == model.User && in.Text == prompt.Text("publish-reminder", nil) {
			n++
		}
	}
	return n
}

func TestPublishReminderContinuesOnceAndAllowsSilence(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	out, err := f.Runtime.Handle(t.Context(), f.Turn)
	testkit.RequireNoError(t, err)
	requests := f.ModelRequests()
	if len(requests) != 2 || reminderCount(requests[0].Inputs) != 0 || reminderCount(requests[1].Inputs) != 1 || out.Status.State != Idle || f.VisibleMessageCount() != 0 {
		t.Fatal("silent turn did not receive exactly one private reminder")
	}
	_, err = f.Restart().Handle(t.Context(), f.Turn)
	testkit.RequireNoError(t, err)
	if len(f.ModelRequests()) != 2 {
		t.Fatal("redelivery repeated reminder")
	}
	next := nextStateTurn(f, "next-user", event.UserMessage)
	next.Event.MessageID = f.Turn.Event.MessageID
	_, err = f.Runtime.Handle(t.Context(), next)
	testkit.RequireNoError(t, err)
	requests = f.ModelRequests()
	if len(requests) != 4 || reminderCount(requests[3].Inputs) != 2 {
		t.Fatal("next user turn did not receive its own reminder")
	}
}

func TestPublishReminderCanRecoverMissingReply(t *testing.T) {
	var calls atomic.Int32
	f, _ := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !testkit.DrainRequestBody(t, r) {
			return
		}
		if calls.Add(1) == 2 {
			completeModel(w, []any{stateTestCall("send", "conversation_publish", `{"conversation_id":"chat","text":"Delivered after reminder"}`)})
			return
		}
		completeModel(w, nil)
	}))
	_, err := f.Runtime.Handle(t.Context(), f.Turn)
	testkit.RequireNoError(t, err)
	if calls.Load() != 3 || f.VisibleMessageCount() != 1 || reminderCount(f.ModelRequests()[1].Inputs) != 1 {
		t.Fatal("reminder did not permit a normal publish tool call")
	}
}

func TestPublishAttemptSuppressesReminderEvenOnFailure(t *testing.T) {
	for _, args := range []string{`{"conversation_id":"chat","text":"sent"}`, `{"conversation_id":"missing","text":"failed"}`} {
		t.Run(args, func(t *testing.T) {
			var calls atomic.Int32
			f, _ := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !testkit.DrainRequestBody(t, r) {
					return
				}
				if calls.Add(1) == 1 {
					completeModel(w, []any{stateTestCall("send", "conversation_publish", args)})
					return
				}
				completeModel(w, nil)
			}))
			_, err := f.Runtime.Handle(t.Context(), f.Turn)
			testkit.RequireNoError(t, err)
			if calls.Load() != 2 || reminderCount(f.ModelRequests()[1].Inputs) != 0 {
				t.Fatal("attempted publication triggered missing-call reminder")
			}
		})
	}
}

func TestPublishReminderSurvivesRestart(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	estimate := e.options.Estimate
	var estimates int
	e.options.Estimate = func(ctx context.Context, id core.ID, inputs []model.Input, limit int) (core.Money, error) {
		estimates++
		if estimates == 2 {
			return core.Money{}, core.NewError(core.Unavailable, "interrupt before reminder dispatch")
		}
		return estimate(ctx, id, inputs, limit)
	}
	_, err := e.Handle(t.Context(), f.Turn)
	testkit.RequireCode(t, err, core.Unavailable)
	cp, err := f.Checkpoints.Load(t.Context(), f.Owner.OwnerID, f.Turn.Event.AssistantID)
	testkit.RequireNoError(t, err)
	s, err := decodeState(cp)
	testkit.RequireNoError(t, err)
	if s.Current == nil || !s.Publish.Reminded || s.Current.Finished || reminderCount(s.Inputs) != 1 {
		t.Fatal("reminder input and marker were not committed together")
	}
	_, err = f.Restart().Handle(t.Context(), f.Turn)
	testkit.RequireNoError(t, err)
	if len(f.ModelRequests()) != 2 || reminderCount(f.ModelRequests()[1].Inputs) != 1 {
		t.Fatal("restart repeated or lost reminder")
	}
}

func TestPublishReminderDoesNotWakeNonUserEventsOrBypassStepLimit(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	_, err := f.Runtime.Handle(t.Context(), nextStateTurn(f, "scheduled", event.ScheduleDue))
	testkit.RequireNoError(t, err)
	if len(f.ModelRequests()) != 1 {
		t.Fatal("non-user event received reminder")
	}
	e := f.Runtime.(*Engine)
	e.options.MaxSteps = 1
	out, err := e.Handle(t.Context(), f.Turn)
	testkit.RequireNoError(t, err)
	if len(f.ModelRequests()) != 2 || out.Status.State != Waiting {
		t.Fatal("reminder bypassed step limit", out.Status)
	}
}

func TestPublishReminderFollowsUserEventAcrossExecutions(t *testing.T) {
	for _, kind := range []event.Kind{event.ResumeRequested, event.ShellTasksRunning, event.ShellTaskFinished, event.OperationCompleted, event.ScheduleDue} {
		t.Run(string(kind), func(t *testing.T) {
			var calls atomic.Int32
			f, _ := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !testkit.DrainRequestBody(t, r) {
					return
				}
				if calls.Add(1) == 1 {
					completeModel(w, []any{stateTestCall("read", "conversation_read", `{"conversation_id":"chat"}`)})
				} else {
					completeModel(w, nil)
				}
			}))
			e := f.Runtime.(*Engine)
			e.options.MaxSteps = 1
			out, err := e.Handle(t.Context(), f.Turn)
			testkit.RequireNoError(t, err)
			if out.Status.WaitingReason != "step_limit" || calls.Load() != 1 {
				t.Fatal("did not suspend before completion", out.Status)
			}
			// A queued user message is not yet the boundary for this continuation.
			expeditedMessage(t, e, f.Owner, "queued for later")
			continuation := nextStateTurn(f, "continuation", kind)
			if kind == event.ShellTasksRunning || kind == event.ShellTaskFinished || kind == event.OperationCompleted {
				continuation.Event.OperationID = "background-operation"
			}
			_, err = f.Restart().Handle(t.Context(), continuation)
			testkit.RequireNoError(t, err)
			requests := f.ModelRequests()
			if len(requests) != 3 || reminderCount(requests[1].Inputs) != 0 || reminderCount(requests[2].Inputs) != 1 {
				t.Fatal("continuation lost the user-event reminder", len(requests))
			}
			// Once remembered, later events and restarts cannot replenish the reminder.
			_, err = f.Restart().Handle(t.Context(), nextStateTurn(f, "later", event.ResumeRequested))
			testkit.RequireNoError(t, err)
			if calls.Load() != 4 {
				t.Fatal("later execution repeated reminder", calls.Load())
			}
			cp, err := f.Checkpoints.Load(t.Context(), f.Owner.OwnerID, f.Turn.Event.AssistantID)
			testkit.RequireNoError(t, err)
			saved, err := decodeState(cp)
			testkit.RequireNoError(t, err)
			if saved.Publish.UserEventID != f.Turn.Event.ID || !saved.Publish.Reminded || saved.Publish.Attempted {
				t.Fatal("wrong user-event boundary", saved.Publish)
			}
		})
	}
}

func TestPublishAttemptSurvivesCompletedTurnAndNewUserResetsIt(t *testing.T) {
	for _, conversation := range []string{"chat", "missing"} {
		t.Run(conversation, func(t *testing.T) {
			var calls atomic.Int32
			f, _ := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !testkit.DrainRequestBody(t, r) {
					return
				}
				if calls.Add(1) == 1 {
					completeModel(w, []any{stateTestCall("send", "conversation_publish", `{"conversation_id":"`+conversation+`","text":"progress"}`)})
				} else {
					completeModel(w, nil)
				}
			}))
			_, err := f.Runtime.Handle(t.Context(), f.Turn)
			testkit.RequireNoError(t, err)
			_, err = f.Restart().Handle(t.Context(), nextStateTurn(f, "continued", event.ResumeRequested))
			testkit.RequireNoError(t, err)
			if calls.Load() != 3 || reminderCount(f.ModelRequests()[2].Inputs) != 0 {
				t.Fatal("previous attempt was forgotten")
			}
			m := expeditedMessage(t, f.Runtime.(*Engine), f.Owner, "a new request")
			next := nextStateTurn(f, "new-user", event.UserMessage)
			next.Event.MessageID, next.Event.MessageSequence = m.ID, m.Sequence
			_, err = f.Restart().Handle(t.Context(), next)
			testkit.RequireNoError(t, err)
			if calls.Load() != 5 || reminderCount(f.ModelRequests()[4].Inputs) != 1 {
				t.Fatal("new user event did not reset reminder")
			}
		})
	}
}

func TestPublishReminderSurvivesContextCompression(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	_, err := f.Runtime.Handle(t.Context(), f.Turn)
	testkit.RequireNoError(t, err)
	compression := nextStateTurn(f, "compress", event.ContextCompressionRequested)
	compression.Event.Manual = true
	_, err = f.Restart().Handle(t.Context(), compression)
	testkit.RequireNoError(t, err)
	cp, err := f.Checkpoints.Load(t.Context(), f.Owner.OwnerID, f.Turn.Event.AssistantID)
	testkit.RequireNoError(t, err)
	saved, err := decodeState(cp)
	testkit.RequireNoError(t, err)
	if reminderCount(saved.Inputs) != 0 || !saved.Publish.Reminded {
		t.Fatal("compression did not preserve marker independently of reminder text")
	}
	before := len(f.ModelRequests())
	_, err = f.Restart().Handle(t.Context(), nextStateTurn(f, "continued", event.ResumeRequested))
	testkit.RequireNoError(t, err)
	if len(f.ModelRequests()) != before+1 {
		t.Fatal("compression replenished reminder")
	}
}

func TestPublishReminderUrgentMessageResetsPriorAttemptOrReminder(t *testing.T) {
	for _, published := range []bool{false, true} {
		t.Run(fmt.Sprint(published), func(t *testing.T) {
			started := make(chan struct{})
			var calls atomic.Int32
			f, _ := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !testkit.DrainRequestBody(t, r) {
					return
				}
				switch calls.Add(1) {
				case 1:
					if published {
						completeModel(w, []any{stateTestCall("send", "conversation_publish", `{"conversation_id":"chat","text":"progress"}`)})
					} else {
						completeModel(w, nil)
					}
				case 2:
					close(started)
					<-r.Context().Done()
				default:
					completeModel(w, nil)
				}
			}))
			e := f.Runtime.(*Engine)
			done := make(chan error, 1)
			go func() { _, err := e.Handle(t.Context(), f.Turn); done <- err }()
			awaitExpedite(t, started)
			m := expeditedMessage(t, e, f.Owner, "urgent new request")
			requestExpedite(t, e, f.Owner, m)
			testkit.RequireNoError(t, awaitExpedite(t, done))
			if calls.Load() != 4 {
				t.Fatal("new urgent message did not get its own reminder", calls.Load())
			}
			cp, err := f.Checkpoints.Load(t.Context(), f.Owner.OwnerID, f.Turn.Event.AssistantID)
			testkit.RequireNoError(t, err)
			saved, err := decodeState(cp)
			testkit.RequireNoError(t, err)
			source := scheduler.UserMessageEvent(f.Owner.OwnerID, f.Turn.Event.AssistantID, m)
			if saved.Publish.UserEventID != source.ID || saved.Publish.MessageSequence != m.Sequence || !saved.Publish.Reminded || saved.Publish.Attempted {
				t.Fatal("urgent user boundary not saved", saved.Publish)
			}
		})
	}
}
