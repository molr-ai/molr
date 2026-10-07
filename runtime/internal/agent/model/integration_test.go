package model

import (
	"context"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

func TestTDDModelIdempotentGenerateDoesNotChargeTwice(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/model", newContractFixture)
	ctx := testkit.Context(t)
	first, err := f.Client.Generate(ctx, f.Request)
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, first.Close())
	second, err := f.Client.Generate(ctx, f.Request)
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, second.Close())
	if f.TransportCalls() != 1 {
		t.Fatal("same model operation generated twice")
	}
	changed := f.Request
	changed.Inputs = []Input{{Role: System, Text: "changed input"}}
	_, err = f.Client.Generate(ctx, changed)
	testkit.RequireCode(t, err, core.Conflict)
	if f.TransportCalls() != 1 {
		t.Fatal("conflicting retry reached provider")
	}
}
func TestTDDModelCanceledRequestNeverDispatches(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/model", newContractFixture)
	ctx, cancel := context.WithCancel(testkit.Context(t))
	cancel()
	_, err := f.Client.Generate(ctx, f.Request)
	testkit.RequireCode(t, err, core.Canceled)
	if f.TransportCalls() != 0 {
		t.Fatal("canceled generation reached provider")
	}
}
func TestTDDModelIntegerPricingRejectsOverflowAndRoundsUp(t *testing.T) {
	value, err := charge(&Pricing{Currency: "USD", InputMicrosPerMillion: 1}, Usage{InputTokens: 1})
	testkit.RequireNoError(t, err)
	if value.Micros != 1 {
		t.Fatal("fractional micro charge rounded down")
	}
	_, err = charge(nil, Usage{})
	testkit.RequireCode(t, err, core.FailedPrecondition)
	_, err = charge(&Pricing{Currency: "USD", InputMicrosPerMillion: 1 << 62}, Usage{InputTokens: 1 << 62})
	testkit.RequireCode(t, err, core.InvalidArgument)
	_, err = charge(&Pricing{Currency: "USD"}, Usage{InputTokens: 1, CachedTokens: 2})
	testkit.RequireCode(t, err, core.InvalidArgument)
}

type controlledAuthorization func(context.Context, core.Principal, policy.CheckRequest) error

func (f controlledAuthorization) Require(ctx context.Context, p core.Principal, r policy.CheckRequest) error {
	return f(ctx, p, r)
}
func TestTDDModelLateResponseRetainsSpendAfterExecutionRevocation(t *testing.T) {
	ctx := testkit.Context(t)
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "late-receipt.db"))
	testkit.RequireNoError(t, err)
	defer db.Close()
	var revoked atomic.Bool
	var settled atomic.Int64
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte(`{"id":"late-result","choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`))
	}))
	defer server.Close()
	authorizer := controlledAuthorization(func(ctx context.Context, p core.Principal, _ policy.CheckRequest) error {
		if revoked.Load() && p.Kind == core.ActorAssistant {
			return core.NewError(core.Forbidden, "execution revoked")
		}
		return p.Validate()
	})
	operations := operation.NewService(db, operation.Options{Authorizer: authorizer})
	execution := core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: "assistant", OperationID: "operation"}
	client := NewClient(db, Options{Operations: operations, Authorizer: authorizer, Resolve: func(context.Context, core.Principal, core.ID) (Provider, error) {
		return Provider{Model: Model{ID: "model", Name: "test", ResourceID: "resource"}, Endpoint: server.URL, Pricing: &Pricing{Currency: "USD", InputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 1000000}}, nil
	}, CheckReservation: func(context.Context, Request, Provider) error { return nil }, CheckDestination: func(context.Context, core.Execution, core.ID, string) error { return nil }, HTTPClient: server.Client(), Settle: func(ctx context.Context, e core.Execution, r Receipt) error {
		settled.Store(r.Charged.Micros)
		return nil
	}})
	done := make(chan error, 1)
	go func() {
		_, err := client.Generate(ctx, Request{Execution: execution, ModelID: "model", ReservationID: "reservation", Inputs: []Input{{Role: User, Text: "hello"}}, MaxOutputTokens: 16})
		done <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("provider request never started")
	}
	revoked.Store(true)
	close(release)
	select {
	case err := <-done:
		testkit.RequireCode(t, err, core.Forbidden)
	case <-ctx.Done():
		t.Fatal("generation did not finish")
	}
	receipt, err := NewReceipts(db).ByOperation(ctx, "owner", "operation")
	testkit.RequireNoError(t, err)
	if !receipt.OutcomeKnown || receipt.Charged == nil || receipt.Charged.Micros != 3 || settled.Load() != 3 {
		t.Fatalf("late response lost known spend: %+v settled=%d", receipt, settled.Load())
	}
}

func TestTDDModelScopedGrantUsesConfiguredEndpointForCatalogAndDispatch(t *testing.T) {
	ctx := testkit.Context(t)
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "scoped-model.db"))
	testkit.RequireNoError(t, err)
	defer db.Close()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"id":"scoped-result","choices":[{"message":{"role":"assistant","content":"allowed"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer server.Close()
	permissions, err := policy.NewService(db, policy.Options{ValidatePrincipal: func(ctx context.Context, p core.Principal) error { return p.Validate() }, ResourceActions: func(context.Context, core.Principal, core.ID) ([]string, error) {
		return []string{"read", "execute"}, nil
	}, Owns: func(ctx context.Context, p core.Principal, o core.Object) error {
		if p.OwnerID != "owner" {
			return core.NewError(core.Forbidden, "foreign object")
		}
		return nil
	}})
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, policy.NewRepository(db).InsertGrant(ctx, policy.Grant{ID: "grant", OwnerID: "owner", AssistantID: "assistant", ResourceID: "resource", Actions: []policy.Action{policy.Read, policy.Execute}, Scope: policy.Scope{ModelID: "model", PathPrefixes: []string{"/v1"}}, Version: 1}))
	execution := core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: "assistant", OperationID: "operation"}
	descriptor := Model{ID: "model", Name: "test", ResourceID: "resource"}
	endpoint := server.URL + "/v1/chat/completions"
	client := NewClient(db, Options{Operations: operation.NewService(db, operation.Options{Authorizer: permissions}), Authorizer: permissions, ListModels: func(context.Context, core.Principal) ([]Model, error) { return []Model{descriptor}, nil }, Resolve: func(context.Context, core.Principal, core.ID) (Provider, error) {
		return Provider{Model: descriptor, Endpoint: endpoint, Pricing: &Pricing{Currency: "USD"}}, nil
	}, HTTPClient: server.Client(), CheckDestination: func(context.Context, core.Execution, core.ID, string) error { return nil }, CheckReservation: func(context.Context, Request, Provider) error { return nil }})
	listed, err := client.List(ctx, execution.Principal)
	testkit.RequireNoError(t, err)
	if len(listed) != 1 {
		t.Fatal("path scoped model was omitted")
	}
	request := Request{Execution: execution, ModelID: "model", ReservationID: "reservation", Inputs: []Input{{Role: User, Text: "hello"}}, MaxOutputTokens: 16}
	stream, err := client.Generate(ctx, request)
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, stream.Close())
	endpoint = server.URL + "/outside"
	request.Execution.OperationID = "denied-operation"
	_, err = client.Generate(ctx, request)
	testkit.RequireCode(t, err, core.Forbidden)
	if calls.Load() != 1 {
		t.Fatal("out-of-scope model endpoint reached provider")
	}
}

func TestModelReplayRejectsChangedProviderIdentity(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/model", newContractFixture)
	ctx := testkit.Context(t)
	first, err := f.Client.Generate(ctx, f.Request)
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, first.Close())
	client := f.Client.(*client)
	resolve := client.options.Resolve
	client.options.Resolve = func(ctx context.Context, p core.Principal, id core.ID) (Provider, error) {
		provider, err := resolve(ctx, p, id)
		provider.Model.ResourceID = "different-provider"
		return provider, err
	}
	// The approved destination and serialized request body stay identical. A
	// changed resource still must not inherit the old operation's result/state.
	client.options.CheckDestination = func(context.Context, core.Execution, core.ID, string) error { return nil }
	_, err = client.Generate(ctx, f.Request)
	testkit.RequireCode(t, err, core.Conflict)
	if f.TransportCalls() != 1 {
		t.Fatal("provider identity change dispatched a duplicate request")
	}
}
