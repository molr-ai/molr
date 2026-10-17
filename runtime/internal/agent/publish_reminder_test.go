package agent

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/prompt"
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
	if s.Current == nil || !s.Current.PublishReminded || s.Current.Finished || reminderCount(s.Inputs) != 1 {
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
