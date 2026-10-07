package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/tests/testkit"
)

// Use a deterministic model transport that completes a turn without publishing.
// Seed ChatSentinel in raw IM history, but not in memory or event inputs.
// FailNextCommit targets the execution persistence transaction. Restart rebuilds
// the real runtime over its retained store, never a mock Runtime implementation.
type contractFixture struct {
	Runtime             Runtime
	Context             ContextBuilder
	Checkpoints         Checkpoints
	Owner, Other        core.Principal
	Turn                Turn
	ChatSentinel        string
	ModelRequests       func() []model.Request
	VisibleMessageCount func() int
	FailNextCommit      func()
	Restart             func() Runtime
}

var newContractFixture func(*testing.T) contractFixture

func TestTDDAgentHandlesEventAndResumesCommittedCheckpoint(t *testing.T) {
	f := testkit.RequireFactory(t, "agent", newContractFixture)
	ctx := testkit.Context(t)
	out, err := f.Runtime.Handle(ctx, f.Turn)
	testkit.RequireNoError(t, err)
	if out.Checkpoint.LastEventID != f.Turn.Event.ID || out.Checkpoint.OwnerID != f.Owner.OwnerID || out.Checkpoint.AssistantID != f.Turn.Event.AssistantID || out.Checkpoint.Sequence == 0 {
		t.Fatalf("checkpoint = %+v", out.Checkpoint)
	}
	saved, err := f.Checkpoints.Load(ctx, f.Owner.OwnerID, f.Turn.Event.AssistantID)
	testkit.RequireNoError(t, err)
	if saved.LastEventID != out.Checkpoint.LastEventID || saved.Sequence != out.Checkpoint.Sequence {
		t.Fatalf("outcome not committed: %+v", saved)
	}
	status, err := f.Runtime.Status(ctx, f.Owner, f.Turn.Event.AssistantID)
	testkit.RequireNoError(t, err)
	if status.AssistantID != f.Turn.Event.AssistantID || status.State != out.Status.State {
		t.Fatalf("status = %+v", status)
	}
	restarted := f.Restart()
	before := len(f.ModelRequests())
	again, err := restarted.Handle(ctx, f.Turn)
	testkit.RequireNoError(t, err)
	if again.Checkpoint.LastEventID != saved.LastEventID || len(f.ModelRequests()) != before {
		t.Fatal("redelivered committed event repeated model execution")
	}
	if f.VisibleMessageCount() != 0 {
		t.Fatal("internal completion was automatically published into chat")
	}
}
func TestTDDAgentContextExcludesImplicitChatHistory(t *testing.T) {
	f := testkit.RequireFactory(t, "agent", newContractFixture)
	ctx := testkit.Context(t)
	inputs, err := f.Context.Build(ctx, f.Turn)
	testkit.RequireNoError(t, err)
	if len(inputs) == 0 {
		t.Fatal("context lacks system/event input")
	}
	for _, input := range inputs {
		if strings.Contains(input.Text, f.ChatSentinel) {
			t.Fatal("raw chat history was injected without a tool read")
		}
	}
	other := f.Turn
	other.Event.OwnerID = f.Other.OwnerID
	_, err = f.Context.Build(ctx, other)
	testkit.RequireCode(t, err, core.Forbidden)
}
func TestTDDAgentAuthorizationStopAndFailedCommitRecovery(t *testing.T) {
	f := testkit.RequireFactory(t, "agent", newContractFixture)
	ctx := testkit.Context(t)
	_, err := f.Runtime.Status(ctx, f.Other, f.Turn.Event.AssistantID)
	if c := core.CodeOf(err); c != core.NotFound && c != core.Forbidden {
		t.Fatalf("foreign status: %v", err)
	}
	err = f.Runtime.Stop(ctx, f.Other, f.Turn.Event.AssistantID)
	if c := core.CodeOf(err); c != core.NotFound && c != core.Forbidden {
		t.Fatalf("foreign stop: %v", err)
	}
	invalid := f.Turn
	invalid.Event.OwnerID = f.Other.OwnerID
	_, err = f.Runtime.Handle(ctx, invalid)
	testkit.RequireCode(t, err, core.Forbidden)
	f.FailNextCommit()
	_, err = f.Runtime.Handle(ctx, f.Turn)
	if err == nil {
		t.Fatal("uncommitted outcome acknowledged")
	}
	retry := f.Restart()
	out, err := retry.Handle(ctx, f.Turn)
	testkit.RequireNoError(t, err)
	if out.Checkpoint.LastEventID != f.Turn.Event.ID {
		t.Fatal("failed turn was not recoverable")
	}
	testkit.RequireNoError(t, retry.Stop(ctx, f.Owner, f.Turn.Event.AssistantID))
	status, err := retry.Status(ctx, f.Owner, f.Turn.Event.AssistantID)
	testkit.RequireNoError(t, err)
	if status.State != Idle {
		t.Fatalf("interrupted loop should be idle: %+v", status)
	}
}
func TestTDDCheckpointCompareAndSwapAndOwnerIsolation(t *testing.T) {
	f := testkit.RequireFactory(t, "agent", newContractFixture)
	ctx := testkit.Context(t)
	cp := Checkpoint{OwnerID: f.Owner.OwnerID, AssistantID: f.Turn.Event.AssistantID, ExecutionID: f.Turn.Principal.ExecutionID, FormatVersion: 1, Sequence: 1, Context: json.RawMessage(`{"step":1}`), LastEventID: f.Turn.Event.ID, SavedAt: time.Now()}
	testkit.RequireNoError(t, f.Checkpoints.Save(ctx, cp, 0))
	loaded, err := f.Checkpoints.Load(ctx, cp.OwnerID, cp.AssistantID)
	testkit.RequireNoError(t, err)
	if loaded.Sequence != 1 || string(loaded.Context) != string(cp.Context) {
		t.Fatalf("loaded = %+v", loaded)
	}
	cp.Sequence = 2
	cp.Context = json.RawMessage(`{"step":2}`)
	testkit.RequireNoError(t, f.Checkpoints.Save(ctx, cp, 1))
	stale := cp
	stale.Sequence = 3
	stale.Context = json.RawMessage(`{"stale":true}`)
	testkit.RequireCode(t, f.Checkpoints.Save(ctx, stale, 1), core.Conflict)
	loaded, err = f.Checkpoints.Load(ctx, cp.OwnerID, cp.AssistantID)
	testkit.RequireNoError(t, err)
	if loaded.Sequence != 2 || string(loaded.Context) != string(cp.Context) {
		t.Fatal("stale save replaced checkpoint")
	}
	_, err = f.Checkpoints.Load(ctx, f.Other.OwnerID, cp.AssistantID)
	if c := core.CodeOf(err); c != core.NotFound && c != core.Forbidden {
		t.Fatalf("foreign checkpoint = %v", err)
	}
}
func TestTDDTurnValidationSourceAndCheckpointFormat(t *testing.T) {
	// This is a pure validation test and deliberately has no implementation fixture.
	base := Turn{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, Event: event.AssistantEvent{ID: "event", OwnerID: "owner", AssistantID: "assistant", Kind: event.ResumeRequested, CreatedAt: time.Now()}}
	testkit.RequireNoError(t, base.Validate())
	for _, tc := range []struct {
		name   string
		mutate func(*Turn)
		code   core.Code
	}{
		{"missing principal", func(x *Turn) { x.Principal = core.Principal{} }, core.Unauthenticated},
		{"assistant mismatch", func(x *Turn) { x.Event.AssistantID = "another" }, core.Forbidden},
		{"invalid event", func(x *Turn) { x.Event.Kind = "invalid" }, core.InvalidArgument},
		{"zero checkpoint format", func(x *Turn) { x.Checkpoint = &Checkpoint{OwnerID: "owner", AssistantID: "assistant"} }, core.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) { v := base; tc.mutate(&v); testkit.RequireCode(t, v.Validate(), tc.code) })
	}
}
