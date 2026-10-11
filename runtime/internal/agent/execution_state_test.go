package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/tests/testkit"
)

func stateTestCall(id, name, args string) any {
	return map[string]any{"id": id, "type": "function", "function": map[string]string{"name": name, "arguments": args}}
}
func nextStateTurn(f contractFixture, id string, kind event.Kind) Turn {
	next := f.Turn
	next.Checkpoint = nil
	next.Event = event.AssistantEvent{ID: core.ID(id), OwnerID: f.Owner.OwnerID, AssistantID: f.Turn.Event.AssistantID, Kind: kind, CreatedAt: time.Now()}
	next.Principal.ExecutionID = core.ID("run-" + id)
	if kind == event.ScheduleDue {
		next.Event.ScheduleID = "schedule"
		next.Event.OccurrenceAt = time.Now()
		next.Event.TaskID = "scheduled-task"
	}
	return next
}
func TestNewScheduledEventContinuesFailedExecutionFacts(t *testing.T) {
	var calls atomic.Int32
	body := make(chan string, 4)
	f, db := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body <- string(b)
		switch calls.Add(1) {
		case 1:
			completeModel(w, []any{stateTestCall("read", "conversation_read", `{"conversation_id":"chat","limit":1}`)})
		case 2:
			completeModel(w, []any{stateTestCall("publish", "conversation_publish", `{"conversation_id":"chat","text":"confirmed side effect"}`)})
		case 3:
			http.Error(w, "failed provider", 503)
		default:
			completeModel(w, nil)
		}
	}))
	e := f.Runtime.(*Engine)
	_, err := e.Handle(t.Context(), f.Turn)
	if err == nil {
		t.Fatal("expected provider failure")
	}
	cp, err := f.Checkpoints.Load(t.Context(), f.Owner.OwnerID, "assistant")
	testkit.RequireNoError(t, err)
	saved, err := decodeState(cp)
	testkit.RequireNoError(t, err)
	raw, _ := json.Marshal(saved.Inputs)
	if !strings.Contains(string(raw), "confirmed side effect") || !strings.Contains(string(raw), f.ChatSentinel) {
		t.Fatal("failure lost confirmed progress")
	}
	var eventRecord map[string]json.RawMessage
	testkit.RequireNoError(t, db.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Get(ctx, progressBucket, "owner/event", &eventRecord)
	}))
	if _, ok := eventRecord["Inputs"]; ok {
		t.Fatal("event still duplicates complete context")
	}
	next := nextStateTurn(f, "scheduled-next", event.ScheduleDue)
	_, err = f.Restart().Handle(t.Context(), next)
	testkit.RequireNoError(t, err)
	for i := 0; i < 3; i++ {
		<-body
	}
	request := <-body
	if !strings.Contains(request, "confirmed side effect") || !strings.Contains(request, f.ChatSentinel) || !strings.Contains(request, "execution handoff") {
		t.Fatal("schedule restored old checkpoint", request)
	}
	if calls.Load() != 4 || f.VisibleMessageCount() != 1 {
		t.Fatal("effects replayed", calls.Load(), f.VisibleMessageCount())
	}
	_, err = e.Handle(t.Context(), f.Turn)
	testkit.RequireCode(t, err, core.Canceled)
	status, err := e.Status(t.Context(), f.Owner, "assistant")
	testkit.RequireNoError(t, err)
	if status.ExecutionID != next.Principal.ExecutionID || calls.Load() != 4 {
		t.Fatal("stale event replaced new state")
	}
}
func TestHandoffUsesCommittedToolReceiptAndSkipsUnstartedCalls(t *testing.T) {
	var calls atomic.Int32
	request := make(chan string, 2)
	f, _ := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		request <- string(b)
		if calls.Add(1) == 1 {
			completeModel(w, []any{stateTestCall("done", "conversation_publish", `{"conversation_id":"chat","text":"committed once"}`), stateTestCall("pending", "conversation_publish", `{"conversation_id":"chat","text":"must not publish"}`)})
		} else {
			completeModel(w, nil)
		}
	}))
	e := f.Runtime.(*Engine)
	e.options.PrepareToolResult = func(_ context.Context, _ core.Execution, r tools.Result) (tools.Result, error) {
		return r, core.NewError(core.Unavailable, "artifact write interrupted")
	}
	_, err := e.Handle(t.Context(), f.Turn)
	if err == nil {
		t.Fatal("expected artifact failure")
	}
	e.options.PrepareToolResult = nil
	_, err = e.Handle(t.Context(), nextStateTurn(f, "new-message", event.ResumeRequested))
	testkit.RequireNoError(t, err)
	<-request
	b := <-request
	if !strings.Contains(b, "committed once") || !strings.Contains(b, "not_started") || !strings.Contains(b, "pending") {
		t.Fatal("handoff lost tool outcomes", b)
	}
	if f.VisibleMessageCount() != 1 || calls.Load() != 2 {
		t.Fatal("abandoned tool queue executed")
	}
}
func TestUnknownOperationSurvivesStopAndLateCompletionIsReconciled(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	call := model.ToolCall{ID: "same-call-id", Name: "file_write", Arguments: json.RawMessage(`{"path":"output","text":"value"}`)}
	response := model.Input{Role: model.Assistant, Calls: []model.ToolCall{call}}
	p := turnProgress{Inputs: []model.Input{{Role: model.System, Text: loopInstructions}, response}, Response: &response}
	testkit.RequireNoError(t, e.saveProgress(t.Context(), f.Turn, &p, progressIfChanged))
	id := stableID(string(f.Turn.Event.ID), "tool", call.ID)
	op := operation.Operation{ID: id, OwnerID: f.Owner.OwnerID, AssistantID: "assistant", ExecutionID: f.Turn.Principal.ExecutionID, Action: call.Name, State: operation.Running, Version: 1}
	testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "agent.operations", persistence.Key(f.Owner.OwnerID, id), op)
	}))
	testkit.RequireNoError(t, e.Stop(t.Context(), f.Owner, "assistant"))
	_, err := e.Handle(t.Context(), nextStateTurn(f, "after-stop", event.ResumeRequested))
	testkit.RequireNoError(t, err)
	cp, err := f.Checkpoints.Load(t.Context(), f.Owner.OwnerID, "assistant")
	testkit.RequireNoError(t, err)
	s, err := decodeState(cp)
	testkit.RequireNoError(t, err)
	if len(s.PendingEvidence) != 1 {
		t.Fatal("unknown effects discarded")
	}
	b, _ := json.Marshal(s.Inputs)
	if !strings.Contains(string(b), "side_effects_possible") || !strings.Contains(string(b), "running") {
		t.Fatal("unknown effect not visible")
	}
	op.State = operation.Succeeded
	op.Result = json.RawMessage(`{"Text":"late confirmed result"}`)
	testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "agent.operations", persistence.Key(f.Owner.OwnerID, id), op); err != nil {
			return err
		}
		s.Inputs = append(s.Inputs, model.Input{Role: model.Assistant, Calls: []model.ToolCall{call}}, model.Input{Role: model.Tool, ReplyTo: call.ID, ToolOperationID: "different-operation", Text: `{"Text":"unrelated result"}`})
		cp.Context, _ = json.Marshal(s)
		cp.Sequence++
		return e.checkpoints.Save(ctx, cp, cp.Sequence-1)
	}))
	_, err = f.Restart().Handle(t.Context(), nextStateTurn(f, "after-receipt", event.ResumeRequested))
	testkit.RequireNoError(t, err)
	cp, err = f.Checkpoints.Load(t.Context(), f.Owner.OwnerID, "assistant")
	testkit.RequireNoError(t, err)
	s, err = decodeState(cp)
	testkit.RequireNoError(t, err)
	b, _ = json.Marshal(s.Inputs)
	if len(s.PendingEvidence) != 0 || !strings.Contains(string(b), "late confirmed result") || !strings.Contains(string(b), "unrelated result") {
		t.Fatal("late receipt not reconciled or wrong reply replaced", string(b))
	}
}
func TestStaleCheckpointWriteCannotOverwriteNewExecution(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	old := turnProgress{Inputs: []model.Input{{Role: model.System, Text: loopInstructions}, {Role: model.User, Text: "old progress"}}}
	testkit.RequireNoError(t, e.saveProgress(t.Context(), f.Turn, &old, progressIfChanged))
	_, err := e.Handle(t.Context(), nextStateTurn(f, "new", event.ResumeRequested))
	testkit.RequireNoError(t, err)
	old.Inputs = append(old.Inputs, model.Input{Role: model.Assistant, Text: "late stale write"})
	err = e.saveProgress(t.Context(), f.Turn, &old, progressIfChanged)
	testkit.RequireCode(t, err, core.Canceled)
	cp, err := f.Checkpoints.Load(t.Context(), f.Owner.OwnerID, "assistant")
	testkit.RequireNoError(t, err)
	if strings.Contains(string(cp.Context), "late stale write") {
		t.Fatal("stale writer overwrote canonical state")
	}
}
func TestUnupgradedProgressIsRejected(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, progressBucket, "owner/event", map[string]any{"Inputs": []any{}})
	}))
	err := f.Runtime.(*Engine).rejectSuperseded(t.Context(), f.Turn)
	testkit.RequireCode(t, err, core.FailedPrecondition)
}

func TestHandoffUsesOnlyCheckpointedModelOutput(t *testing.T) {
	f, db := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	p := turnProgress{Inputs: []model.Input{{Role: model.System, Text: loopInstructions}, {Role: model.User, Text: "in-flight work"}}}
	testkit.RequireNoError(t, e.saveProgress(t.Context(), f.Turn, &p, progressIfChanged))
	id := modelOperationID(f.Turn, 0)
	testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "agent.operations", persistence.Key(f.Owner.OwnerID, id), operation.Operation{ID: id, OwnerID: f.Owner.OwnerID, AssistantID: "assistant", ExecutionID: f.Turn.Principal.ExecutionID, Action: "model.generate", State: operation.Succeeded})
	}))
	next := nextStateTurn(f, "after-model-commit", event.ResumeRequested)
	_, restored, err := e.prepareExecution(t.Context(), next)
	testkit.RequireNoError(t, err)
	if restored.Response != nil {
		t.Fatal("invented an uncheckpointed model response")
	}
	_, err = f.Restart().Handle(t.Context(), next)
	testkit.RequireNoError(t, err)
	if len(f.ModelRequests()) != 1 || f.VisibleMessageCount() != 0 {
		t.Fatal("handoff did not request fresh model output")
	}
}

func TestHandoffRetainsProviderUsageBaseline(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	response := model.Input{Role: model.Assistant, Calls: []model.ToolCall{{ID: "pending", Name: "conversation_read", Arguments: json.RawMessage(`{"conversation_id":"chat","limit":1}`)}}}
	p := turnProgress{Inputs: []model.Input{{Role: model.System, Text: loopInstructions}, response}, Response: &response}
	p.Usage = &contextUsage{ModelID: "model", Tokens: 30000, Inputs: usageInputs(p.Inputs, model.StateSource{}), Tools: contextHash(p.Definitions)}
	testkit.RequireNoError(t, e.saveProgress(t.Context(), f.Turn, &p, progressIfChanged))
	_, next, err := e.prepareExecution(t.Context(), nextStateTurn(f, "usage-next", event.ScheduleDue))
	testkit.RequireNoError(t, err)
	tokens, source := contextTokens(next, "model", model.StateSource{})
	if next.Usage == nil || tokens < 30000 || source != "provider_with_estimate" {
		t.Fatal("handoff discarded actual provider usage", tokens, source)
	}
}
