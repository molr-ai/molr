package agent

import (
	"context"
	"io"
	"net/http"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/tests/testkit"
)

func TestTriggerTracksTurnChangesAndStop(t *testing.T) {
	started := make(chan struct{}, 4)
	release := make(chan struct{}, 4)
	f, _ := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		started <- struct{}{}
		select {
		case <-release:
			completeModel(w, nil)
		case <-r.Context().Done():
		}
	}))
	e := f.Runtime.(*Engine)
	for i, kind := range []event.Kind{event.UserMessage, event.ResumeRequested, event.UserMessage} {
		turn := f.Turn
		turn.Event.ID = core.ID([]string{"first", "scheduled-resume", "next-message"}[i])
		turn.Principal.ExecutionID = turn.Event.ID
		turn.Event.Kind = kind
		turn.Event.MessageID = ""
		if kind == event.UserMessage {
			m := expeditedMessage(t, e, f.Owner, string(turn.Event.ID))
			turn.Event.MessageID = m.ID
			turn.Event.MessageSequence = m.Sequence
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { _, err := e.Handle(ctx, turn); done <- err }()
		awaitExpedite(t, started)
		status, err := e.Status(t.Context(), f.Owner, "assistant")
		testkit.RequireNoError(t, err)
		if status.State != Working || status.TriggerMessageID != turn.Event.MessageID {
			t.Fatal("incorrect turn association", status)
		}
		if i == 2 {
			testkit.RequireNoError(t, e.Stop(t.Context(), f.Owner, "assistant"))
			if err := awaitExpedite(t, done); err == nil {
				t.Fatal("stopped execution succeeded")
			}
		} else {
			release <- struct{}{}
			if kind == event.UserMessage {
				awaitExpedite(t, started) // one-time missing-publication reminder
				release <- struct{}{}
			}
			testkit.RequireNoError(t, awaitExpedite(t, done))
		}
		if i < 2 {
			events, err := realtime.NewStore(e.store).Replay(t.Context(), f.Owner.OwnerID, realtime.Query{Topics: []string{"assistant.state"}})
			testkit.RequireNoError(t, err)
			// Each completed turn must invalidate clients after persisting its idle status.
			if len(events.Items) != 2*(i+1) || events.Items[len(events.Items)-1].Object.ID != "assistant" {
				t.Fatal("turn completion did not publish a status update", events)
			}
		}
		cancel()
		status, err = e.Status(t.Context(), f.Owner, "assistant")
		testkit.RequireNoError(t, err)
		if status.State != Idle || status.TriggerMessageID != "" {
			t.Fatal("inactive turn retained association", status)
		}
	}
}

func TestTriggerClearedOnFailureAndRestoredOnRetry(t *testing.T) {
	started := make(chan struct{}, 2)
	f, _ := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		started <- struct{}{}
		<-r.Context().Done()
	}))
	e := f.Runtime.(*Engine)
	// Fail before dispatch, so the same event is safe to retry after restart.
	e.options.ResolveModel = func(context.Context, core.Principal, core.ID) (core.ID, error) {
		return "", core.NewError(core.Unavailable, "model temporarily unavailable")
	}
	_, err := e.Handle(t.Context(), f.Turn)
	testkit.RequireCode(t, err, core.Unavailable)
	status, err := e.Status(t.Context(), f.Owner, "assistant")
	testkit.RequireNoError(t, err)
	if status.State != Waiting || status.TriggerMessageID != "" {
		t.Fatal("failure retained trigger", status)
	}
	e = f.Restart().(*Engine)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := e.Handle(ctx, f.Turn); done <- err }()
	awaitExpedite(t, started)
	status, err = e.Status(t.Context(), f.Owner, "assistant")
	testkit.RequireNoError(t, err)
	if status.TriggerMessageID != f.Turn.Event.MessageID {
		t.Fatal("retry lost trigger", status)
	}
	cancel()
	if err := awaitExpedite(t, done); err == nil {
		t.Fatal("canceled execution succeeded")
	}
	status, err = e.Status(t.Context(), f.Owner, "assistant")
	testkit.RequireNoError(t, err)
	if status.State != Waiting || status.TriggerMessageID != "" {
		t.Fatal("failure retained active trigger", status)
	}

	page, err := e.options.Conversations.Messages(t.Context(), f.Owner, conversation.MessageQuery{ConversationID: "chat", MessageID: f.Turn.Event.MessageID})
	testkit.RequireNoError(t, err)
	if page.Items[0].ReadAt != nil {
		t.Fatal("retry falsely marked input read")
	}
}
