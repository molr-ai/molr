package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/tests/testkit"
)

func TestPermissionRequestWaitsBeforeDependentCallsAndContinuesAfterRestart(t *testing.T) {
	var modelCalls, requests atomic.Int32
	f, db := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !testkit.DrainRequestBody(t, r) {
			return
		}
		if modelCalls.Add(1) == 1 {
			completeModel(w, []any{
				stateTestCall("request", "permission_request", `{"resource_kind":"storage","resource_id":"storage","resource_description":"Reports","actions":["read"],"path_prefixes":["/reports"],"expires_at":"2099-01-01T00:00:00Z","reason":"Analyze reports"}`),
				stateTestCall("dependent", "conversation_publish", `{"conversation_id":"chat","text":"must not run before approval"}`),
			})
		} else {
			completeModel(w, nil)
		}
	}))
	e := f.Runtime.(*Engine)
	e.options.Dependencies.Tools = tools.NewExecutor(db, tools.Options{Authorizer: e.options.Dependencies.Policy, Operations: operation.NewService(db, operation.Options{Authorizer: e.options.Dependencies.Policy}), Conversations: conversation.NewService(db, conversation.Options{Authorizer: e.options.Dependencies.Policy}), PermissionRequest: func(ctx context.Context, execution core.Execution, input policy.PermissionRequestInput) (json.RawMessage, error) {
		_, err := e.options.Conversations.Publish(ctx, execution, conversation.PublishRequest{Mutation: core.Mutation{IdempotencyKey: "permission-card"}, ConversationID: "chat", Content: conversation.Content{Kind: conversation.Card, Card: &conversation.CardContent{Kind: conversation.PermissionRequestCard, TargetID: "request", Title: "Read reports"}}})
		if err != nil {
			return nil, err
		}
		requests.Add(1)
		return json.RawMessage(`{"ID":"request","State":"pending"}`), nil
	}})
	out, err := e.Handle(t.Context(), f.Turn)
	testkit.RequireNoError(t, err)
	if out.Status.State != Waiting || out.Status.WaitingReason != "permission_pending" || modelCalls.Load() != 1 || requests.Load() != 1 || f.VisibleMessageCount() != 1 {
		t.Fatalf("did not pause: %+v, calls=%d requests=%d", out.Status, modelCalls.Load(), requests.Load())
	}
	cp, err := e.checkpoints.Load(t.Context(), f.Owner.OwnerID, f.Turn.Event.AssistantID)
	testkit.RequireNoError(t, err)
	if !strings.Contains(string(cp.Context), "not_started") || strings.Contains(string(cp.Context), `"side_effects_possible":true`) {
		t.Fatal("dependent call outcome was not preserved safely")
	}
	// A new decision event resumes saved context; it does not replay the request.
	restarted := NewRuntime(db, e.options)
	_, err = restarted.Handle(t.Context(), nextStateTurn(f, "permission-decision", event.ResumeRequested))
	testkit.RequireNoError(t, err)
	if modelCalls.Load() != 3 || requests.Load() != 1 || f.VisibleMessageCount() != 1 {
		t.Fatal("resume replayed effects or permission card suppressed reminder")
	}
}

func TestInvalidPermissionRequestReturnsReasonWithoutWaiting(t *testing.T) {
	var calls atomic.Int32
	f, db := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		switch calls.Add(1) {
		case 1:
			completeModel(w, []any{stateTestCall("request", "permission_request", `{"resource_kind":"storage","resource_id":"missing","resource_description":"Reports","actions":["read"],"path_prefixes":["/reports"],"expires_at":"2099-01-01T00:00:00Z","reason":"Analyze reports"}`)})
		case 2:
			var request struct {
				Messages []struct{ Role, Content string }
			}
			testkit.RequireNoError(t, json.Unmarshal(raw, &request))
			found := false
			for _, message := range request.Messages {
				if message.Role == "tool" && strings.Contains(message.Content, `"ok":false`) && strings.Contains(message.Content, `"field":"resource_id"`) && strings.Contains(message.Content, "resource does not exist") {
					found = true
				}
			}
			if !found {
				t.Error("model did not receive the specific permission failure")
			}
			completeModel(w, []any{stateTestCall("report", "conversation_publish", `{"conversation_id":"chat","text":"The requested resource does not exist. Please connect it first."}`)})
		default:
			completeModel(w, nil)
		}
	}))
	e := f.Runtime.(*Engine)
	e.options.Dependencies.Tools = tools.NewExecutor(db, tools.Options{Authorizer: e.options.Dependencies.Policy, Operations: operation.NewService(db, operation.Options{Authorizer: e.options.Dependencies.Policy}), Conversations: conversation.NewService(db, conversation.Options{Authorizer: e.options.Dependencies.Policy}), PermissionRequest: func(context.Context, core.Execution, policy.PermissionRequestInput) (json.RawMessage, error) {
		return nil, core.Invalid("resource_id", "resource does not exist; refresh permission_list")
	}})
	out, err := e.Handle(t.Context(), f.Turn)
	testkit.RequireNoError(t, err)
	if out.Status.WaitingReason == "permission_pending" || calls.Load() != 3 || f.VisibleMessageCount() != 1 {
		t.Fatalf("invalid permission stopped model correction: %+v calls=%d messages=%d", out.Status, calls.Load(), f.VisibleMessageCount())
	}
}
