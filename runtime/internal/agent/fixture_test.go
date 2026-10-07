package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/tests/testkit"
)

type fixtureAuthorizer struct{}

func (fixtureAuthorizer) Require(ctx context.Context, p core.Principal, _ policy.CheckRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return p.Validate()
}

// The budget dependency delegates to the real policy SQLite repository; tests
// control authorization externally while exercising actual reservation records.
type fixtureBudget struct{ repo policy.Repository }

func (b fixtureBudget) Reserve(ctx context.Context, r policy.ReserveRequest) (policy.Reservation, error) {
	return b.repo.Reserve(ctx, r)
}
func (b fixtureBudget) Settle(ctx context.Context, e core.Execution, r policy.SettleRequest) (policy.Reservation, error) {
	return b.repo.Settle(ctx, e.Principal.OwnerID, r)
}
func (b fixtureBudget) MarkUnknown(ctx context.Context, e core.Execution, id core.ID) error {
	return b.repo.SetReservationState(ctx, e.Principal.OwnerID, id, "unknown")
}
func (b fixtureBudget) Release(ctx context.Context, e core.Execution, id core.ID) error {
	return b.repo.SetReservationState(ctx, e.Principal.OwnerID, id, "released")
}
func (b fixtureBudget) Usage(ctx context.Context, p core.Principal, q policy.UsageQuery) (policy.Usage, error) {
	return b.repo.Usage(ctx, p.OwnerID, q)
}
func init() {
	newContractFixture = func(t *testing.T) contractFixture { f, _ := makeLoopFixture(t, nil); return f }
}
func makeLoopFixture(t *testing.T, handler http.Handler, protocols ...model.Protocol) (contractFixture, *sqlitestore.Store) {
	t.Helper()
	ctx := testkit.Context(t)
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "agent.db"))
	testkit.RequireNoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	owner := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
	other := core.Principal{OwnerID: "other", SubjectID: "other", Kind: core.ActorOwner, SessionID: "other-session"}
	principal := core.Principal{OwnerID: owner.OwnerID, SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}
	sentinel := "raw-chat-not-implicit-context"
	repo := conversation.NewRepository(db)
	testkit.RequireNoError(t, repo.Insert(ctx, conversation.Conversation{ID: "chat", OwnerID: owner.OwnerID, PeerID: "assistant", Version: 1}))
	_, err = repo.Append(ctx, owner.OwnerID, conversation.Message{ID: "history", ConversationID: "chat", Sender: conversation.User, SenderID: owner.SubjectID, Content: conversation.Content{Kind: conversation.Text, Text: sentinel}, CreatedAt: time.Now()})
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.assistants", persistence.Key(owner.OwnerID, "assistant"), struct {
			OwnerID core.ID
			State   string
			Budget  core.AssistantBudget
		}{owner.OwnerID, "active", core.AssistantBudget{Limit: core.Money{Currency: "USD", Micros: 1000000}, Period: "daily"}})
	}))
	if handler == nil {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var request map[string]any
			if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&request) != nil {
				http.Error(w, "invalid request", 400)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"provider-completion","choices":[{"message":{"role":"assistant","content":"private completion"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3}}`))
		})
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	var mu sync.Mutex
	requests := []model.Request{}
	authorizer := fixtureAuthorizer{}
	operations := operation.NewService(db, operation.Options{Authorizer: authorizer})
	provider := model.Provider{Model: model.Model{ID: "model", Name: "test-model", ResourceID: "resource", SupportsTools: true}, Endpoint: server.URL, Pricing: &model.Pricing{Currency: "USD", InputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 1000000}}
	if len(protocols) > 0 {
		provider.Protocol = protocols[0]
	}
	chat := conversation.NewService(db, conversation.Options{Authorizer: authorizer})
	models := model.NewClient(db, model.Options{Dispatched: func(ctx context.Context, request model.Request) error {
		var ids []core.ID
		for _, input := range request.Inputs {
			ids = append(ids, input.ReadMessageIDs...)
		}
		if len(ids) == 0 {
			return nil
		}
		return chat.ReadUserMessages(ctx, request.Execution, ids)
	}, Operations: operations, Authorizer: authorizer, HTTPClient: server.Client(), Resolve: func(context.Context, core.Principal, core.ID) (model.Provider, error) { return provider, nil }, CheckDestination: func(ctx context.Context, e core.Execution, id core.ID, target string) error {
		if target != server.URL {
			return core.NewError(core.Forbidden, "unexpected endpoint")
		}
		return nil
	}, CheckReservation: func(ctx context.Context, req model.Request, provider model.Provider) error {
		var row struct{ Reservation policy.Reservation }
		if err := db.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return tx.Get(ctx, "security.reservations", persistence.Key(req.Execution.Principal.OwnerID, req.ReservationID), &row)
		}); err != nil {
			return err
		}
		if row.Reservation.OperationID != req.Execution.OperationID {
			return core.NewError(core.Forbidden, "reservation mismatch")
		}
		mu.Lock()
		defer mu.Unlock()
		data, _ := json.Marshal(req)
		var copy model.Request
		_ = json.Unmarshal(data, &copy)
		requests = append(requests, copy)
		return nil
	}})
	toolExecutor := tools.NewExecutor(db, tools.Options{Authorizer: authorizer, Operations: operations, Conversations: conversation.NewService(db, conversation.Options{Authorizer: authorizer})})
	options := Options{Conversations: chat, Dependencies: Dependencies{Models: models, Tools: toolExecutor, Policy: authorizer, Budget: fixtureBudget{policy.NewRepository(db)}}, ResolveModel: func(context.Context, core.Principal, core.ID) (core.ID, error) { return "model", nil }, Estimate: func(context.Context, core.ID, []model.Input, int) (core.Money, error) {
		return core.Money{Currency: "USD", Micros: 1000}, nil
	}, ConversationID: func(context.Context, core.Principal, core.ID) (core.ID, error) { return "chat", nil }}
	engine := NewRuntime(db, options)
	testkit.RequireNoError(t, engine.Lifecycle(ctx, owner, "assistant", "initialize"))
	visible := realtime.NewStore(db)
	fixture := contractFixture{Runtime: engine, Context: NewContextBuilder(options), Checkpoints: NewCheckpoints(db), Owner: owner, Other: other, Turn: Turn{Principal: principal, Event: event.AssistantEvent{ID: "event", OwnerID: owner.OwnerID, AssistantID: "assistant", Kind: event.UserMessage, MessageID: "history", CreatedAt: time.Now()}}, ChatSentinel: sentinel, ModelRequests: func() []model.Request { mu.Lock(); defer mu.Unlock(); return append([]model.Request{}, requests...) }, VisibleMessageCount: func() int {
		page, err := visible.Replay(ctx, owner.OwnerID, realtime.Query{})
		testkit.RequireNoError(t, err)
		count := 0
		for _, visible := range page.Items {
			if visible.Topic == "message.created" {
				count++
			}
		}
		return count
	}, FailNextCommit: func() { db.FailNextCommit(errors.New("injected checkpoint commit failure")) }, Restart: func() Runtime { return NewRuntime(db, options) }}
	return fixture, db
}
