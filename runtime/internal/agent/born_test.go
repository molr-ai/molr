package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/tests/testkit"
)

func TestBornPublishesThroughNormalLoopAndDoesNotRepeatAfterRestart(t *testing.T) {
	var calls atomic.Int32
	f, _ := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		message := map[string]any{"role": "assistant", "content": "private completion"}
		finish := "stop"
		if calls.Add(1) == 1 {
			// The fixture already has a user message; born still follows the normal loop.
			if !strings.Contains(string(body), "You have just been created.") || !strings.Contains(string(body), "conversation_publish") {
				t.Error("birth instructions missing")
			}
			message["tool_calls"] = []any{map[string]any{"id": "greeting", "type": "function", "function": map[string]any{
				"name": "conversation_publish", "arguments": `{"conversation_id":"chat","text":"Hello, I am ready to help."}`,
			}}}
			finish = "tool_calls"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "born-completion", "choices": []any{map[string]any{"message": message, "finish_reason": finish}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 3}})
	}))
	turn := f.Turn
	turn.Event.Kind = event.Born
	turn.Event.MessageID = ""
	turn.Event.MessageSequence = 0
	out, err := f.Runtime.Handle(t.Context(), turn)
	testkit.RequireNoError(t, err)
	if out.Status.State != Idle || calls.Load() != 2 || f.VisibleMessageCount() != 1 {
		t.Fatalf("birth outcome: %+v, calls=%d, messages=%d", out, calls.Load(), f.VisibleMessageCount())
	}
	_, err = f.Restart().Handle(t.Context(), turn)
	testkit.RequireNoError(t, err)
	if calls.Load() != 2 || f.VisibleMessageCount() != 1 {
		t.Fatal("completed birth replayed")
	}
}
