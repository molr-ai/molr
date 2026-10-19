package mcpclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/quota"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type allow struct{}

func (allow) Require(_ context.Context, p core.Principal, _ policy.CheckRequest) error {
	return p.Validate()
}

type fixture struct {
	s        *Service
	p        core.Principal
	server   *mcp.Server
	http     *httptest.Server
	calls    atomic.Int32
	deny     atomic.Bool
	executor tools.Executor
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{p: core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}}
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "db"))
	must(t, err)
	t.Cleanup(func() { db.Close() })
	f.server = mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	f.add(t, "lookup", "Look up fixture data", func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		f.calls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "fixture result"}}, StructuredContent: map[string]any{"ok": true}}, nil
	})
	f.http = httptest.NewTLSServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return f.server }, &mcp.StreamableHTTPOptions{Stateless: true}))
	t.Cleanup(f.http.Close)
	f.s = New(db, Options{Authorize: func(ctx context.Context, p core.Principal, write bool) error {
		if write && p.OwnerID != "owner" {
			return denied()
		}
		return p.RequireOwner()
	}, IsAdministrator: func(_ context.Context, p core.Principal) (bool, error) { return p.OwnerID == "owner", nil }, CheckConnection: func(ctx context.Context, p core.Principal, id core.ID) error {
		if p.OwnerID != "owner" {
			return denied()
		}
		return p.Validate()
	}, CheckTool: func(ctx context.Context, p core.Principal, id core.ID) error {
		if f.deny.Load() {
			return denied()
		}
		return p.Validate()
	}, HTTPClient: func(context.Context, core.Principal, core.Execution, core.ID, string) (*http.Client, error) {
		return f.http.Client(), nil
	}})
	f.executor = tools.NewExecutor(db, tools.Options{Authorizer: allow{}, Operations: operation.NewService(db, operation.Options{Authorizer: allow{}}), ExtensionDefinitions: f.s.Definitions, ExtensionValidate: f.s.ValidateCall, ExtensionPrepare: f.s.PrepareCall, ExtensionExecute: f.s.Execute})
	return f
}
func (f *fixture) add(t *testing.T, name, desc string, fn mcp.ToolHandler) {
	t.Helper()
	f.server.AddTool(&mcp.Tool{Name: name, Description: desc, InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"],"additionalProperties":false}`)}, fn)
}
func (f *fixture) create(t *testing.T) View {
	t.Helper()
	v, err := f.s.Save(t.Context(), f.p, SaveRequest{Name: "Fixture", Endpoint: f.http.URL + "/mcp", Auth: "none", Enabled: true, CallLimit: 10, Concurrent: 2, TimeoutSeconds: 5, Mutation: core.Mutation{IdempotencyKey: string(newID())}})
	must(t, err)
	return v
}
func (f *fixture) refresh(t *testing.T, v View) View {
	t.Helper()
	out, err := f.s.Refresh(t.Context(), f.p, Request{ID: v.ID, Mutation: core.Mutation{IdempotencyKey: string(newID()), ExpectedVersion: v.Version}})
	must(t, err)
	return out
}
func (f *fixture) save(t *testing.T, v View, ids ...core.ID) View {
	t.Helper()
	out, err := f.s.Save(t.Context(), f.p, SaveRequest{ID: v.ID, Name: v.Name, Endpoint: v.Endpoint, Auth: v.Auth, Header: v.Header, Enabled: v.Enabled, EnabledTools: ids, CallLimit: v.CallLimit, Concurrent: v.Concurrent, TimeoutSeconds: v.TimeoutSeconds, Mutation: core.Mutation{IdempotencyKey: string(newID()), ExpectedVersion: v.Version}})
	must(t, err)
	return out
}
func call(v View) tools.Call {
	return tools.Call{Name: string(v.Tools[0].ID), Arguments: json.RawMessage(`{"query":"fixture"}`), Execution: core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "execution"}, AssistantID: "assistant", OperationID: newID()}}
}
func TestDiscoveryApprovalExecutionReplayAndChangedDefinition(t *testing.T) {
	f := setup(t)
	v := f.refresh(t, f.create(t))
	if len(v.Tools) != 1 || v.Tools[0].Enabled {
		t.Fatal("discovered tools must start disabled", v)
	}
	c := call(v)
	_, err := f.executor.Execute(t.Context(), c)
	if err == nil || f.calls.Load() != 0 {
		t.Fatal("disabled tool executed")
	}
	v = f.save(t, v, v.Tools[0].ID)
	c = call(v)
	r, err := f.executor.Execute(t.Context(), c)
	must(t, err)
	if !strings.Contains(string(r.Content), "fixture result") {
		t.Fatal(string(r.Content))
	}
	replay, err := f.executor.Execute(t.Context(), c)
	must(t, err)
	if string(replay.Content) != string(r.Content) || f.calls.Load() != 1 {
		t.Fatal("operation replay dispatched again")
	}
	used, err := quota.CallTotals(t.Context(), f.s.store, "connection", "owner", v.ID, "", time.Time{}, time.Time{})
	must(t, err)
	if used.Calls != 1 || used.Active != 0 {
		t.Fatal(used)
	}
	history, err := f.s.History(t.Context(), f.p, v.ID)
	must(t, err)
	if len(history) != 1 || history[0].State != "succeeded" {
		t.Fatal(history)
	}
	v = f.refresh(t, v)
	if !v.Tools[0].Enabled || string(v.Tools[0].ID) != c.Name {
		t.Fatal("unchanged tool lost identity")
	}
	old := v.Tools[0].ID
	f.add(t, "lookup", "Changed behavior", func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})
	v = f.refresh(t, v)
	if v.Tools[0].Enabled || v.Tools[0].ID == old {
		t.Fatal("changed tool inherited approval")
	}
	rsc, err := resource.NewRepository(f.s.store).Get(t.Context(), f.p.OwnerID, old)
	must(t, err)
	if rsc.State != resource.Disconnected {
		t.Fatal("old grant resource remains active")
	}
	_, err = f.executor.Execute(t.Context(), c)
	if err == nil {
		t.Fatal("old tool still executable")
	}
}
func TestIdentityIsolationQuotaAndPermissionRevocation(t *testing.T) {
	f := setup(t)
	v := f.refresh(t, f.create(t))
	v.CallLimit = 1
	v = f.save(t, v, v.Tools[0].ID)
	other := f.p
	other.OwnerID = "other"
	other.SubjectID = "other"
	rows, err := f.s.List(t.Context(), other)
	must(t, err)
	if len(rows) != 0 {
		t.Fatal("private connection leaked")
	}
	c := call(v)
	f.deny.Store(true)
	_, err = f.executor.Execute(t.Context(), c)
	if err == nil {
		t.Fatal("revoked permission executed")
	}
	f.deny.Store(false)
	_, err = f.executor.Execute(t.Context(), c)
	must(t, err)
	c.Execution.OperationID = newID()
	_, err = f.executor.Execute(t.Context(), c)
	if core.CodeOf(err) != core.BudgetExceeded {
		t.Fatalf("quota: %v", err)
	}
	if f.calls.Load() != 1 {
		t.Fatal("quota did not prevent remote call")
	}
}
func TestUnknownOutcomeNeverReplayedAndConcurrencyReleased(t *testing.T) {
	f := setup(t)
	f.add(t, "lookup", "Slow side effect", func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		f.calls.Add(1)
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
		}
		return &mcp.CallToolResult{IsError: true}, nil
	})
	v := f.refresh(t, f.create(t))
	v.TimeoutSeconds = 1
	v.Concurrent = 1
	v = f.save(t, v, v.Tools[0].ID)
	c := call(v)
	_, err := f.executor.Execute(t.Context(), c)
	if err == nil {
		t.Fatal("timeout succeeded")
	}
	_, err = f.executor.Execute(t.Context(), c)
	if err == nil {
		t.Fatal("unknown replay succeeded")
	}
	if f.calls.Load() != 1 {
		t.Fatal("unknown call retried")
	}
	h, err := f.s.History(t.Context(), f.p, v.ID)
	must(t, err)
	if len(h) != 1 || h[0].State != "unknown" {
		t.Fatal(h)
	}
	used, err := quota.CallTotals(t.Context(), f.s.store, "connection", "owner", v.ID, "", time.Time{}, time.Time{})
	must(t, err)
	if used.Active != 0 || used.Unknown != 1 {
		t.Fatal(used)
	}
}
func TestConcurrentCallsAndDisableCancel(t *testing.T) {
	f := setup(t)
	started := make(chan struct{}, 1)
	f.add(t, "lookup", "Wait", func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		f.calls.Add(1)
		started <- struct{}{}
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
		}
		return &mcp.CallToolResult{IsError: true}, nil
	})
	v := f.refresh(t, f.create(t))
	v.Concurrent = 1
	v = f.save(t, v, v.Tools[0].ID)
	done := make(chan error, 1)
	go func() { _, err := f.executor.Execute(t.Context(), call(v)); done <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("call did not start")
	}
	_, err := f.executor.Execute(t.Context(), call(v))
	if core.CodeOf(err) != core.BudgetExceeded {
		t.Fatalf("concurrency %v", err)
	}
	v.Enabled = false
	f.save(t, v, v.Tools[0].ID)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("disabled running call succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("disable did not cancel")
	}
}
func TestSchemaAndArgumentsValidation(t *testing.T) {
	for _, raw := range []string{`null`, `{"type":"array"}`, `{"type":"object","properties":{"url":{"$ref":"https://example.invalid/schema"}}}`, `{"type":"object","properties":{"key":{"type":"string","x-mcp-header":"secret"}}}`} {
		if _, err := schema(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	resolved, err := schema(json.RawMessage(`{"type":"object","properties":{"items":{"type":"array","items":{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"]}}},"required":["items"]}`))
	must(t, err)
	must(t, resolved.Validate(map[string]any{"items": []any{map[string]any{"id": float64(3)}}}))
	if resolved.Validate(map[string]any{"items": []any{map[string]any{"id": "bad"}}}) == nil {
		t.Fatal("nested validation bypass")
	}
	f := setup(t)
	v := f.refresh(t, f.create(t))
	v = f.save(t, v, v.Tools[0].ID)
	c := call(v)
	c.Arguments = json.RawMessage(`{"query":123}`)
	_, err = f.executor.Execute(t.Context(), c)
	if err == nil || f.calls.Load() != 0 {
		t.Fatal("invalid arguments dispatched")
	}
}
func TestOfflineCatalogRemovalAndRecovery(t *testing.T) {
	f := setup(t)
	v := f.refresh(t, f.create(t))
	v = f.save(t, v, v.Tools[0].ID)
	f.http.Close()
	defs, err := f.s.Definitions(t.Context(), f.p, true)
	must(t, err)
	if len(defs) != 1 {
		t.Fatal("offline catalog not cached")
	}
	_, err = f.s.Refresh(t.Context(), f.p, Request{ID: v.ID, Mutation: core.Mutation{IdempotencyKey: "offline", ExpectedVersion: v.Version}})
	if err == nil {
		t.Fatal("offline discovery succeeded")
	}
	rows, err := f.s.List(t.Context(), f.p)
	must(t, err)
	v = rows[0]
	if v.Status != "offline" || len(v.Tools) != 1 {
		t.Fatal(v)
	}
	c, err := f.s.connection(t.Context(), f.p.OwnerID, v.ID)
	must(t, err)
	must(t, f.s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "mcp.calls", "owner/interrupted", CallRecord{ID: "interrupted", OwnerID: "owner", Connection: c, State: "running", CreatedAt: time.Now()})
	}))
	must(t, f.s.Recover(t.Context()))
	h, err := f.s.History(t.Context(), f.p, v.ID)
	must(t, err)
	if h[0].State != "unknown" {
		t.Fatal(h)
	}
	must(t, f.s.Remove(t.Context(), f.p, Request{ID: v.ID, Mutation: core.Mutation{IdempotencyKey: "remove", ExpectedVersion: v.Version}}))
	defs, err = f.s.Definitions(t.Context(), f.p, true)
	must(t, err)
	if len(defs) != 0 {
		t.Fatal("deleted tools visible")
	}
	h, err = f.s.History(t.Context(), f.p, v.ID)
	must(t, err)
	if len(h) != 1 {
		t.Fatal("history deleted")
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestTransportBindingResponseLimitAndRedaction(t *testing.T) {
	base := roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer fixture-secret" {
			t.Fatal("missing authentication")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", (4<<20)+1)))}, nil
	})
	transport := &boundedTransport{base: base, endpoint: "https://example.invalid/mcp", auth: "bearer", secret: core.NewSecret("fixture-secret"), check: func() error { return nil }}
	req, _ := http.NewRequest("POST", "https://other.invalid/mcp", nil)
	if _, err := transport.RoundTrip(req); err == nil {
		t.Fatal("credential destination changed")
	}
	req, _ = http.NewRequest("POST", "https://example.invalid/mcp", nil)
	resp, err := transport.RoundTrip(req)
	must(t, err)
	defer resp.Body.Close()
	if _, err := io.ReadAll(resp.Body); err == nil {
		t.Fatal("unbounded response")
	}
	out := redact(map[string]any{"fixture-secret": []any{"prefix fixture-secret"}}, "fixture-secret")
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "fixture-secret") {
		t.Fatal("credential leaked")
	}
}
func TestSaveIdempotencyAndImmutableEndpoint(t *testing.T) {
	f := setup(t)
	q := SaveRequest{Name: "Fixture", Endpoint: f.http.URL + "/mcp", Auth: "none", Enabled: true, CallLimit: 10, Concurrent: 2, TimeoutSeconds: 5, Mutation: core.Mutation{IdempotencyKey: "same"}}
	a, err := f.s.Save(t.Context(), f.p, q)
	must(t, err)
	b, err := f.s.Save(t.Context(), f.p, q)
	must(t, err)
	if a.ID != b.ID {
		t.Fatal("duplicate connection")
	}
	q.Name = "Changed"
	if _, err := f.s.Save(t.Context(), f.p, q); core.CodeOf(err) != core.Conflict {
		t.Fatal(err)
	}
	q.ID = a.ID
	q.Endpoint = f.http.URL + "/another"
	q.Mutation = core.Mutation{IdempotencyKey: "change", ExpectedVersion: a.Version}
	if _, err := f.s.Save(t.Context(), f.p, q); err == nil {
		t.Fatal("endpoint mutated")
	}
}

func TestCancellationAfterAdmissionReleasesPreparedSlot(t *testing.T) {
	f := setup(t)
	v := f.refresh(t, f.create(t))
	v = f.save(t, v, v.Tools[0].ID)
	c := call(v)
	_, err := f.s.PrepareCall(t.Context(), c)
	must(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = f.s.Execute(ctx, c)
	if err == nil {
		t.Fatal("canceled call succeeded")
	}
	used, err := quota.CallTotals(t.Context(), f.s.store, "connection", "owner", v.ID, "", time.Time{}, time.Time{})
	must(t, err)
	if used.Active != 0 || f.calls.Load() != 0 {
		t.Fatal("prepared slot leaked or request dispatched", used)
	}
}

func TestSameRemoteNameAcrossConnectionsHasSeparateIdentity(t *testing.T) {
	f := setup(t)
	a := f.refresh(t, f.create(t))
	b := f.refresh(t, f.create(t))
	if a.ID == b.ID || a.Tools[0].ID == b.Tools[0].ID {
		t.Fatal("remote tool names collided")
	}
	a = f.save(t, a, a.Tools[0].ID)
	b = f.save(t, b, b.Tools[0].ID)
	for _, v := range []View{a, b} {
		_, err := f.executor.Execute(t.Context(), call(v))
		must(t, err)
		used, err := quota.CallTotals(t.Context(), f.s.store, "connection", "owner", v.ID, "", time.Time{}, time.Time{})
		must(t, err)
		if used.Calls != 1 {
			t.Fatal("connection accounting mixed", used)
		}
	}
}
func TestTransportBindsBackgroundRequestsToOperationDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	transport := &boundedTransport{operationContext: ctx, endpoint: "https://example.invalid/mcp", check: func() error { return nil }, base: roundTrip(func(r *http.Request) (*http.Response, error) {
		select {
		case <-r.Context().Done():
			return nil, r.Context().Err()
		case <-time.After(time.Second):
			t.Error("operation cancellation was not propagated")
			return nil, context.DeadlineExceeded
		}
	})}
	req, _ := http.NewRequest("DELETE", transport.endpoint, nil)
	if _, err := transport.RoundTrip(req); err == nil {
		t.Fatal("canceled request succeeded")
	}
}
