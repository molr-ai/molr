package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/tests/testkit"
)

type resultExecutor struct {
	tools.Executor
	run func(context.Context, tools.Call) (tools.Result, error)
}

func (e resultExecutor) Execute(ctx context.Context, c tools.Call) (tools.Result, error) {
	return e.run(ctx, c)
}

func TestLoopContinuesAfterEveryToolFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failure  error
		fallback bool
	}{
		{"access revoked", core.NewError(core.Forbidden, "storage permission revoked"), false},
		{"tool canceled", context.Canceled, false},
		{"tool timeout", context.DeadlineExceeded, false},
		{"authentication", core.NewError(core.Unauthenticated, "resource authentication expired"), false},
		{"budget", core.NewError(core.BudgetExceeded, "tool allowance exhausted"), false},
		{"rate limited", core.NewError(core.RateLimited, "tool rate limit reached"), false},
		{"unknown write", core.Wrap(core.Unavailable, "receipt unavailable", errors.New("PRIVATE_CAUSE")), false},
		{"internal failure", errors.New("PRIVATE_INTERNAL_ERROR"), false},
		{"decorator failure", context.Canceled, true},
		{"invalid result", core.NewError(core.Internal, "invalid result"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var models, attempts atomic.Int64
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				step := models.Add(1)
				message := map[string]any{"role": "assistant", "content": ""}
				finish := "tool_calls"
				invoke := func(id, name, args string) any {
					return map[string]any{"id": id, "type": "function", "function": map[string]any{"name": name, "arguments": args}}
				}
				switch step {
				case 1:
					message["tool_calls"] = []any{invoke("failing-call", "file_write", `{"path":"out.txt","text":"data","overwrite":false}`), invoke("following-call", "conversation_read", `{"conversation_id":"chat","message_id":"history"}`)}
				case 2:
					var request struct {
						Messages []struct {
							Role    string `json:"role"`
							Content string `json:"content"`
							ReplyTo string `json:"tool_call_id"`
						}
					}
					if err := json.Unmarshal(raw, &request); err != nil {
						t.Error(err)
					}
					results := map[string]string{}
					for _, m := range request.Messages {
						if m.Role == "tool" {
							results[m.ReplyTo] = m.Content
						}
					}
					var failure struct {
						Error tools.Failure `json:"error"`
					}
					if err := json.Unmarshal([]byte(results["failing-call"]), &failure); err != nil {
						t.Errorf("missing error result: %v", err)
					}
					if failure.Error.Code != core.CodeOf(tc.failure) || failure.Error.Message == "" {
						t.Errorf("failure=%+v", failure)
					}
					if !strings.Contains(results["following-call"], "raw-chat-not-implicit-context") {
						t.Error("failure skipped another call in the same batch")
					}
					if strings.Contains(string(raw), "PRIVATE_") {
						t.Error("private error cause reached model")
					}
					if tc.name == "unknown write" && (!failure.Error.SideEffectsPossible || failure.Error.ExecutionState != "unknown") {
						t.Error("unknown side effects not reported")
					}
					message["tool_calls"] = []any{invoke("publish", "conversation_publish", `{"conversation_id":"chat","text":"Tool failed; I can still explain the result."}`)}
				default:
					finish = "stop"
					message["content"] = "handled"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "completion", "choices": []any{map[string]any{"message": message, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 3}})
			})
			f, db := makeLoopFixture(t, handler)
			engine := f.Runtime.(*Engine)
			a := fixtureAuthorizer{}
			ex := tools.NewExecutor(db, tools.Options{Authorizer: a, Operations: operation.NewService(db, operation.Options{Authorizer: a}), Conversations: conversation.NewService(db, conversation.Options{Authorizer: a}), FileWrite: func(context.Context, core.Execution, tools.FileWriteRequest) (json.RawMessage, error) {
				attempts.Add(1)
				return nil, tc.failure
			}})
			engine.options.Dependencies.Tools = ex
			if tc.fallback {
				engine.options.Dependencies.Tools = resultExecutor{Executor: ex, run: func(ctx context.Context, c tools.Call) (tools.Result, error) {
					if c.Name == "file_write" {
						attempts.Add(1)
						if tc.name == "invalid result" {
							return tools.Result{Content: json.RawMessage(`not-json`)}, nil
						}
						return tools.Result{}, tc.failure
					}
					return ex.Execute(ctx, c)
				}}
			}
			out, err := engine.Handle(t.Context(), f.Turn)
			testkit.RequireNoError(t, err)
			if models.Load() != 3 || attempts.Load() != 1 || out.Status.State != Idle {
				t.Fatalf("loop stopped: calls=%d attempts=%d state=%s", models.Load(), attempts.Load(), out.Status.State)
			}
			messages, err := conversation.NewRepository(db).Messages(t.Context(), f.Owner.OwnerID, conversation.MessageQuery{ConversationID: "chat"})
			testkit.RequireNoError(t, err)
			if len(messages.Items) != 2 || messages.Items[1].Sender != conversation.Assistant {
				t.Fatal("agent could not report failure to the user")
			}
		})
	}
}

func TestRuntimeCancellationStillStopsFurtherCalls(t *testing.T) {
	var models atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		models.Add(1)
		io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(`{"id":"completion","choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"cancel","type":"function","function":{"name":"file_write","arguments":"{\"path\":\"out.txt\",\"text\":\"data\",\"overwrite\":false}"}},{"id":"after-cancel","type":"function","function":{"name":"conversation_publish","arguments":"{\"conversation_id\":\"chat\",\"text\":\"must not publish\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":3}}`))
	})
	f, db := makeLoopFixture(t, handler)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	a := fixtureAuthorizer{}
	f.Runtime.(*Engine).options.Dependencies.Tools = tools.NewExecutor(db, tools.Options{Authorizer: a, Operations: operation.NewService(db, operation.Options{Authorizer: a}), Conversations: conversation.NewService(db, conversation.Options{Authorizer: a}), FileWrite: func(context.Context, core.Execution, tools.FileWriteRequest) (json.RawMessage, error) {
		cancel()
		return nil, context.Canceled
	}})
	_, err := f.Runtime.Handle(ctx, f.Turn)
	testkit.RequireCode(t, err, core.Canceled)
	page, err := conversation.NewRepository(db).Messages(t.Context(), f.Owner.OwnerID, conversation.MessageQuery{ConversationID: "chat"})
	testkit.RequireNoError(t, err)
	if models.Load() != 1 || len(page.Items) != 1 {
		t.Fatal("Runtime cancellation did not stop follow-up work")
	}
}
