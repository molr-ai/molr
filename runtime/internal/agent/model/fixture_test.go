package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/buildinfo"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/tests/testkit"
)

type fixtureAuthorizer struct{ revoked *atomic.Bool }

func (a fixtureAuthorizer) Require(ctx context.Context, p core.Principal, r policy.CheckRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if r.ResourceID != "" && a.revoked.Load() {
		return core.NewError(core.Forbidden, "model grant revoked")
	}
	return nil
}
func init() {
	newContractFixture = func(t *testing.T) contractFixture {
		t.Helper()
		db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "model.db"))
		testkit.RequireNoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		var calls atomic.Int64
		var failure, revoked, reservationRevoked, evidence atomic.Bool
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got, want := r.UserAgent(), "molr/"+buildinfo.Version; got != want {
				t.Errorf("User-Agent = %q, want %q", got, want)
			}
			calls.Add(1)
			if failure.Load() {
				connection, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					_ = connection.Close()
				}
				return
			}
			var request map[string]any
			if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || json.NewDecoder(r.Body).Decode(&request) != nil || request["stream"] != true {
				http.Error(w, "invalid request", 400)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"provider-request","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1}}`))
		}))
		t.Cleanup(server.Close)
		owner := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
		other := core.Principal{OwnerID: "other", SubjectID: "other", Kind: core.ActorOwner, SessionID: "other-session"}
		execution := core.Execution{Principal: core.Principal{OwnerID: owner.OwnerID, SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: "assistant", OperationID: "operation"}
		model := Model{ID: "model", Name: "local-test", ResourceID: "model-resource", SupportsTools: true, ContextLimit: 8192}
		options := Options{Operations: operation.NewService(db, operation.Options{Authorizer: fixtureAuthorizer{&revoked}}), Authorizer: fixtureAuthorizer{&revoked}, ListModels: func(context.Context, core.Principal) ([]Model, error) { return []Model{model}, nil }, Resolve: func(ctx context.Context, p core.Principal, id core.ID) (Provider, error) {
			if id != model.ID {
				return Provider{}, core.NewError(core.NotFound, "model not found")
			}
			return Provider{Model: model, Endpoint: server.URL + "/v1/chat/completions", Pricing: &Pricing{Currency: "USD", InputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 1000000}}, nil
		}, CheckReservation: func(context.Context, Request, Provider) error {
			if reservationRevoked.Load() {
				return core.NewError(core.BudgetExceeded, "reservation revoked")
			}
			return nil
		}, HTTPClient: server.Client(), CheckDestination: func(ctx context.Context, e core.Execution, id core.ID, target string) error {
			if target != server.URL+"/v1/chat/completions" || id != model.ResourceID {
				return core.NewError(core.Forbidden, "destination mismatch")
			}
			return nil
		}, Reconcile: func(ctx context.Context, e core.Execution, r Receipt) (Receipt, error) {
			if !evidence.Load() {
				return r, core.NewError(core.FailedPrecondition, "no evidence")
			}
			r.OutcomeKnown = true
			r.Charged = &core.Money{Currency: "USD", Micros: 5}
			return r, nil
		}}
		receipts := NewReceipts(db)
		return contractFixture{Client: NewClient(db, options), Receipts: receipts, Owner: owner, Other: other, Request: Request{Execution: execution, ModelID: model.ID, Inputs: []Input{{Role: System, Text: "Follow the event"}}, MaxOutputTokens: 128, ReservationID: "reservation"}, TransportCalls: func() int { return int(calls.Load()) }, TransportFailure: func() { failure.Store(true) }, SettleProvider: func(core.ID) { evidence.Store(true) }, AwaitReceipt: func(ctx context.Context, id core.ID) (Receipt, error) {
			return receipts.ByOperation(ctx, owner.OwnerID, id)
		}, RevokeModel: func() { revoked.Store(true) }, RevokeReservation: func() { reservationRevoked.Store(true) }}
	}
}
