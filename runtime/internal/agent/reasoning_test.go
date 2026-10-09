package agent

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/tests/testkit"
)

func TestIncompleteDeepSeekCheckpointStopsWithoutRequestOrBudgetHold(t *testing.T) {
	var requests atomic.Int64
	f, db := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "must not reach provider", 400)
	}), model.DeepSeekChat)
	ctx := testkit.Context(t)
	data, err := json.Marshal(struct{ Inputs []model.Input }{[]model.Input{{Role: model.Assistant, Text: "old response without continuation state"}}})
	testkit.RequireNoError(t, err)
	checkpoint := Checkpoint{OwnerID: f.Owner.OwnerID, AssistantID: f.Turn.Event.AssistantID, ExecutionID: "previous", FormatVersion: 1, Sequence: 1, Context: data, LastEventID: "previous-event", SavedAt: time.Now()}
	testkit.RequireNoError(t, f.Checkpoints.Save(ctx, checkpoint, 0))
	_, err = f.Runtime.Handle(ctx, f.Turn)
	testkit.RequireCode(t, err, core.FailedPrecondition)
	if core.ReasonOf(err) != core.ModelContextIncompatible || requests.Load() != 0 {
		t.Fatalf("unexpected failure: %v, requests=%d", err, requests.Load())
	}
	status, err := f.Runtime.Status(ctx, f.Owner, f.Turn.Event.AssistantID)
	testkit.RequireNoError(t, err)
	if status.WaitingReason != string(core.ModelContextIncompatible) {
		t.Fatalf("missing user-actionable status: %+v", status)
	}
	rows, err := persistence.Scan[struct{ Reservation policy.Reservation }](ctx, db, "security.reservations")
	testkit.RequireNoError(t, err)
	if len(rows) != 1 || rows[0].Reservation.State != "released" {
		t.Fatalf("preflight failure retained budget: %+v", rows)
	}
	saved, err := f.Checkpoints.Load(ctx, f.Owner.OwnerID, f.Turn.Event.AssistantID)
	testkit.RequireNoError(t, err)
	state, decodeErr := decodeState(saved)
	if decodeErr != nil || len(state.Inputs) == 0 || state.Inputs[0].Text != "old response without continuation state" || state.Inputs[0].ProviderState != nil {
		t.Fatal("incompatible context was silently discarded")
	}
}

func TestAgentPreservesReasoningAcrossToolsCacheAndRestart(t *testing.T) {
	// Include an explicitly empty value: it must not turn into an absent field.
	reasoning := []string{"private reasoning\n读取消息", "", "private final reasoning", "reminder reasoning", "next event reasoning", "next reminder reasoning"}
	var requests atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []map[string]any `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		step := int(requests.Add(1))
		index := 0
		for _, message := range request.Messages {
			if message["role"] != "assistant" {
				continue
			}
			value, present := message["reasoning_content"]
			if index >= len(reasoning) || !present || value != reasoning[index] {
				t.Errorf("request %d lost reasoning on assistant message %d: %#v", step, index, message)
				http.Error(w, "reasoning_content must be passed back", 400)
				return
			}
			index++
		}
		if index != step-1 {
			t.Errorf("request %d has %d prior assistant messages", step, index)
		}
		message := map[string]any{"role": "assistant", "content": "private completion", "reasoning_content": "next event reasoning"}
		finish := "stop"
		if step <= len(reasoning) {
			message["reasoning_content"] = reasoning[step-1]
		}
		if step <= 2 {
			message["content"] = nil
			message["tool_calls"] = []any{map[string]any{"id": []string{"read-1", "read-2"}[step-1], "type": "function", "function": map[string]any{"name": "conversation_read", "arguments": `{"conversation_id":"chat","limit":10}`}}}
			finish = "tool_calls"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "completion", "choices": []any{map[string]any{"message": message, "finish_reason": finish}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 3}})
	})
	f, db := makeLoopFixture(t, handler, model.DeepSeekChat)
	ctx := testkit.Context(t)
	out, err := f.Runtime.Handle(ctx, f.Turn)
	testkit.RequireNoError(t, err)
	if out.Status.State != Idle || requests.Load() != 4 {
		t.Fatalf("unexpected outcome: %+v calls=%d", out.Status, requests.Load())
	}
	// A new event after a restart must preserve every assistant message, including the final one.
	turn := f.Turn
	turn.Event.ID = "next-event"
	turn.Principal.ExecutionID = "next-run"
	_, err = f.Restart().Handle(ctx, turn)
	testkit.RequireNoError(t, err)
	if requests.Load() != 6 {
		t.Fatalf("requests=%d, want 6", requests.Load())
	}
	page, err := conversation.NewRepository(db).Messages(ctx, f.Owner.OwnerID, conversation.MessageQuery{ConversationID: "chat"})
	testkit.RequireNoError(t, err)
	if len(page.Items) != 1 || f.VisibleMessageCount() != 0 {
		t.Fatal("private reasoning leaked into the conversation")
	}
}
