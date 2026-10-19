package plugins

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
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

type httpAuthorizer struct{}

func (httpAuthorizer) Require(_ context.Context, p core.Principal, _ policy.CheckRequest) error {
	return p.Validate()
}

func TestHTTPThroughToolExecutorReplaysOriginalReceiptAfterSwitch(t *testing.T) {
	s, p, v, a, b := setupHTTP(t)
	selectConnection(t, s, p, v, a.ID)
	authz := httpAuthorizer{}
	ops := operation.NewService(s.store, operation.Options{Authorizer: authz})
	executor := tools.NewExecutor(s.store, tools.Options{Authorizer: authz, Operations: ops, ExtensionDefinitions: s.Definitions, ExtensionValidate: s.ValidateCall, ExtensionPrepare: s.PrepareCall, ExtensionExecute: s.Execute})
	call := httpToolCall(v, "executor-call")
	result, err := executor.Execute(t.Context(), call)
	must(t, err)
	if !strings.Contains(string(result.Content), `"provider":"a"`) {
		t.Fatal("provider A did not execute", string(result.Content))
	}
	selectConnection(t, s, p, v, b.ID)
	replay, err := executor.Execute(t.Context(), call)
	must(t, err)
	if string(result.Content) != string(replay.Content) {
		t.Fatal("replay changed provider")
	}
	calls, _, err := s.connectionUsage(t.Context(), b.ID)
	must(t, err)
	if calls != 0 {
		t.Fatal("replay billed new provider")
	}
	call.Execution.OperationID = "executor-next"
	next, err := executor.Execute(t.Context(), call)
	must(t, err)
	if !strings.Contains(string(next.Content), `"provider":"b"`) {
		t.Fatal("new call did not switch")
	}
}

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(data string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(data)), Header: http.Header{}}
}
func httpPackage() Package {
	schema := Schema{Type: "object", Properties: map[string]Property{"query": {Type: "string"}}, Required: []string{"query"}}
	projection := Projection{Type: "object", Fields: map[string]Projection{"text": {Type: "string", Path: "/text"}}}
	op := HTTPOperation{Method: "POST", Path: "/lookup", InputSchema: schema, Body: map[string]HTTPValue{"q": {Argument: "query"}, "language": {Setting: "language"}}, Result: projection}
	b := op
	b.Method = "GET"
	b.Query = op.Body
	b.Body = nil
	b.Result = Projection{Type: "object", Fields: map[string]Projection{"text": {Type: "string", Path: "/message"}}}
	return Package{Manifest: Manifest{ManifestVersion: 1, PluginAPI: 3, ID: "fixture.http", Name: "HTTP fixture", Version: "1.0.0", Settings: map[string]Setting{"backend": {Type: "connection", Title: "Backend", Providers: []string{"a", "b"}, RequiredOperations: []string{"lookup"}}, "language": {Type: "string", Title: "Language", Default: json.RawMessage(`"en"`), Enum: []string{"en", "zh"}}}, Providers: []HTTPProvider{
		{ID: "a", Name: "Provider A", Origin: "https://a.example", Auth: HTTPAuth{Type: "bearer"}, Operations: map[string]HTTPOperation{"lookup": op}, Test: &HTTPProbe{Operation: "lookup", Arguments: json.RawMessage(`{"query":"probe"}`)}},
		{ID: "b", Name: "Provider B", Origin: "https://b.example", Auth: HTTPAuth{Type: "header", Header: "X-API-Key"}, Operations: map[string]HTTPOperation{"lookup": b}, Test: &HTTPProbe{Operation: "lookup", Arguments: json.RawMessage(`{"query":"probe"}`)}},
	}, Contributes: Contributions{Tools: []Tool{{ID: "lookup", Description: "Read a fixture", Execution: ToolExecution{Type: "http", ConnectionSetting: "backend", Operation: "lookup"}, InputSchema: schema, Capabilities: []string{"http.request"}}}, Pages: []Page{{ID: "settings", Title: "Settings", Entry: "ui.html", Capabilities: []string{"settings.read", "settings.write", "connections.read", "connections.test"}}}}}, Files: map[string]string{"ui.html": "<p>fixture</p>"}}
}
func setupHTTP(t *testing.T) (*Service, core.Principal, Installation, ConnectionView, ConnectionView) {
	t.Helper()
	s, p, _ := fixture(t)
	must(t, s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "auth.bootstrap", "owner", struct{ AccountID core.ID }{p.OwnerID})
	}))
	s.options.IsAdministrator = func(_ context.Context, p core.Principal) (bool, error) { return p.OwnerID == "owner", nil }
	s.options.Authorize = func(_ context.Context, p core.Principal, write bool) error {
		if write && p.OwnerID != "owner" {
			return core.NewError(core.Forbidden, "admin required")
		}
		return p.Validate()
	}
	s.options.AuthorizeSettings = func(_ context.Context, p core.Principal) error { return p.RequireOwner() }
	s.options.CheckConnection = func(_ context.Context, p core.Principal, _ core.ID) error {
		if p.OwnerID == "blocked" {
			return core.NewError(core.Forbidden, "denied")
		}
		return p.Validate()
	}
	vault, err := credential.NewService(s.store, credential.Options{MasterKey: make([]byte, 32), ValidatePrincipal: func(_ context.Context, p core.Principal) error { return p.Validate() }, CheckVerification: func(context.Context, core.Principal, auth.Verification, string) error { return nil }, AuthorizeAccess: func(ctx context.Context, a credential.Access) error {
		p, err := a.EffectivePrincipal()
		if err != nil {
			return err
		}
		c, provider, err := s.ConnectionAccess(ctx, p, a.ResourceID)
		if err != nil {
			return err
		}
		if c.CredentialID != a.CredentialID || provider.Origin != a.Origin {
			return core.NewError(core.Forbidden, "credential mismatch")
		}
		return nil
	}})
	must(t, err)
	s.options.Credentials = vault
	s.options.Broker = vault
	s.options.HTTPClient = func(context.Context, core.Principal, core.Execution, core.ID, string) (*http.Client, error) {
		return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host == "a.example" {
				if req.Method != "POST" || req.Header.Get("Authorization") != "Bearer secret-a" {
					return nil, errors.New("invalid A request")
				}
				var body map[string]any
				if json.NewDecoder(req.Body).Decode(&body) != nil || body["q"] == nil {
					return nil, errors.New("missing body")
				}
				raw, _ := json.Marshal(map[string]any{"text": "A:" + body["q"].(string) + ":secret-a"})
				return response(string(raw)), nil
			}
			if req.Method != "GET" || req.Header.Get("X-API-Key") != "secret-b" {
				return nil, errors.New("invalid B request")
			}
			raw, _ := json.Marshal(map[string]any{"message": "B:" + req.URL.Query().Get("q")})
			return response(string(raw)), nil
		})}, nil
	}
	v, err := s.Install(t.Context(), p, httpPackage())
	must(t, err)
	v, err = s.Configure(t.Context(), p, Configure{ID: v.ID, ExpectedVersion: v.Version, Enabled: true, Grants: Requested(v.Manifest), Bindings: []Binding{{AssistantID: "assistant", Tools: []string{"lookup"}}}})
	must(t, err)
	save := func(id, key string) ConnectionView {
		c, err := s.SaveConnection(t.Context(), p, SaveConnection{PluginID: v.ID, Digest: v.Digest, Name: id, ProviderID: id, Enabled: true, APIKey: core.NewSecret(key), Mutation: core.Mutation{IdempotencyKey: id}, CallLimit: 20, Concurrent: 2, TimeoutSeconds: 10})
		must(t, err)
		return c
	}
	return s, p, v, save("a", "secret-a"), save("b", "secret-b")
}
func selectConnection(t *testing.T, s *Service, p core.Principal, v Installation, id core.ID) UserSettings {
	t.Helper()
	old, err := s.Settings(t.Context(), p, v.ID, v.Digest)
	must(t, err)
	out, err := s.SaveSettings(t.Context(), p, SettingsRequest{ID: v.ID, Digest: v.Digest, ExpectedVersion: old.Version, Values: map[string]any{"backend": string(id), "language": "en"}})
	must(t, err)
	return out
}
func httpToolCall(v Installation, id string) tools.Call {
	e := execution()
	e.OperationID = core.ID(id)
	return tools.Call{Execution: e, Name: ToolName(v, "lookup"), Arguments: json.RawMessage(`{"query":"a & b = \"quoted\""}`)}
}
func TestHTTPProviderSwitchPinsSnapshotAndProtectsCredentials(t *testing.T) {
	s, p, v, a, b := setupHTTP(t)
	selectConnection(t, s, p, v, a.ID)
	defs, err := s.Definitions(t.Context(), execution().Principal, false)
	must(t, err)
	if len(defs) != 1 || defs[0].Name != ToolName(v, "lookup") {
		t.Fatal("HTTP tool requires compute")
	}
	first := httpToolCall(v, "first")
	_, err = s.PrepareCall(t.Context(), first)
	must(t, err)
	selectConnection(t, s, p, v, b.ID)
	data, err := s.Execute(t.Context(), first)
	must(t, err)
	if !strings.Contains(string(data), `"provider":"a"`) || strings.Contains(string(data), "secret-a") {
		t.Fatalf("wrong snapshot or exposed credential: %s", data)
	}
	if _, err = s.Execute(t.Context(), first); err == nil {
		t.Fatal("direct replay made another request")
	}
	second := httpToolCall(v, "second")
	_, err = s.PrepareCall(t.Context(), second)
	must(t, err)
	data, err = s.Execute(t.Context(), second)
	must(t, err)
	if !strings.Contains(string(data), `"provider":"b"`) || !strings.Contains(string(data), "quoted") {
		t.Fatalf("switch/mapping failed: %s", data)
	}
}
func TestPluginSettingsIsolationCASAndDeniedConnection(t *testing.T) {
	s, p, v, a, b := setupHTTP(t)
	one := selectConnection(t, s, p, v, a.ID)
	other := p
	other.OwnerID = "other"
	other.SubjectID = "other"
	selectConnection(t, s, other, v, b.ID)
	current, err := s.Settings(t.Context(), p, v.ID, v.Digest)
	must(t, err)
	if current.Values["backend"] != string(a.ID) {
		t.Fatal("settings leaked across users")
	}
	_, err = s.SaveSettings(t.Context(), p, SettingsRequest{ID: v.ID, Digest: v.Digest, ExpectedVersion: one.Version - 1, Values: one.Values})
	if core.CodeOf(err) != core.Conflict {
		t.Fatal("stale settings accepted", err)
	}
	blocked := other
	blocked.OwnerID = "blocked"
	blocked.SubjectID = "blocked"
	_, err = s.SaveSettings(t.Context(), blocked, SettingsRequest{ID: v.ID, Digest: v.Digest, Values: map[string]any{"backend": string(a.ID)}})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("unauthorized connection accepted", err)
	}
	_, err = s.SaveSettings(t.Context(), p, SettingsRequest{ID: v.ID, Digest: v.Digest, ExpectedVersion: one.Version, Values: map[string]any{"secret": "never"}})
	if core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("unknown setting accepted", err)
	}
	_, err = s.SaveConnection(t.Context(), other, SaveConnection{})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("member configured shared connection", err)
	}
	rows, err := s.Connections(t.Context(), p, v.ID, v.Digest)
	must(t, err)
	raw, _ := json.Marshal(rows)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "CredentialID") {
		t.Fatal("connection view exposed secret binding")
	}
}
func TestHTTPConnectionQuotaProbeAndRevocation(t *testing.T) {
	s, p, v, a, _ := setupHTTP(t)
	selectConnection(t, s, p, v, a.ID)
	_, err := s.SaveConnection(t.Context(), p, SaveConnection{PluginID: v.ID, Digest: v.Digest, ID: a.ID, ProviderID: "a", Name: "a", Enabled: true, Mutation: core.Mutation{IdempotencyKey: "limits", ExpectedVersion: a.Version}, CallLimit: 2, Concurrent: 1, TimeoutSeconds: 10})
	must(t, err)
	first := httpToolCall(v, "held")
	_, err = s.PrepareCall(t.Context(), first)
	must(t, err)
	_, err = s.PrepareCall(t.Context(), httpToolCall(v, "overlap"))
	if core.CodeOf(err) != core.BudgetExceeded {
		t.Fatal("concurrency exceeded", err)
	}
	_, err = s.Execute(t.Context(), first)
	must(t, err)
	probe := TestConnectionRequest{PluginID: v.ID, Digest: v.Digest, ID: a.ID, Mutation: core.Mutation{IdempotencyKey: "probe"}}
	data, err := s.TestConnection(t.Context(), p, probe)
	must(t, err)
	again, err := s.TestConnection(t.Context(), p, probe)
	must(t, err)
	if string(data) != string(again) {
		t.Fatal("probe replay changed")
	}
	_, err = s.PrepareCall(t.Context(), httpToolCall(v, "exhausted"))
	if core.CodeOf(err) != core.BudgetExceeded {
		t.Fatal("call limit exceeded", err)
	}
	_, err = s.Configure(t.Context(), p, Configure{ID: v.ID, ExpectedVersion: v.Version, Enabled: false})
	must(t, err)
	_, err = s.TestConnection(t.Context(), p, probe)
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("disabled plugin test permitted", err)
	}
}
func TestHTTPDisableCancelsInflightAndPreservesUnknown(t *testing.T) {
	s, p, v, a, _ := setupHTTP(t)
	selectConnection(t, s, p, v, a.ID)
	entered := make(chan struct{})
	var requests atomic.Int32
	s.options.HTTPClient = func(context.Context, core.Principal, core.Execution, core.ID, string) (*http.Client, error) {
		return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests.Add(1)
			close(entered)
			<-r.Context().Done()
			return nil, r.Context().Err()
		})}, nil
	}
	call := httpToolCall(v, "cancel")
	_, err := s.PrepareCall(t.Context(), call)
	must(t, err)
	done := make(chan error, 1)
	go func() { _, err := s.Execute(t.Context(), call); done <- err }()
	<-entered
	_, err = s.Configure(t.Context(), p, Configure{ID: v.ID, ExpectedVersion: v.Version, Enabled: false})
	must(t, err)
	if err := <-done; err == nil {
		t.Fatal("inflight request survived revocation")
	}
	record, err := persistence.Load[HTTPCall](t.Context(), s.store, "plugins.http-calls", persistence.Key(p.OwnerID, call.Execution.OperationID))
	must(t, err)
	if record.State != "unknown" || requests.Load() != 1 {
		t.Fatal("unknown outcome lost or retried", record.State)
	}
}
func TestHTTPManifestAndProjectionRejectUnsupportedBehavior(t *testing.T) {
	tests := []func(*Package){
		func(p *Package) { p.Manifest.PluginAPI = 1 },
		func(p *Package) { p.Manifest.Providers[0].Origin = "http://example.com" },
		func(p *Package) { p.Manifest.Providers[0].Auth = HTTPAuth{Type: "header", Header: "Host"} },
		func(p *Package) {
			op := p.Manifest.Providers[0].Operations["lookup"]
			op.Path = "//other.example"
			p.Manifest.Providers[0].Operations["lookup"] = op
		},
		func(p *Package) {
			op := p.Manifest.Providers[0].Operations["lookup"]
			op.Body["bad"] = HTTPValue{Argument: "unknown"}
			p.Manifest.Providers[0].Operations["lookup"] = op
		},
		func(p *Package) {
			op := p.Manifest.Providers[1].Operations["lookup"]
			op.Result = Projection{Type: "string"}
			p.Manifest.Providers[1].Operations["lookup"] = op
		},
	}
	for i, change := range tests {
		p := httpPackage()
		change(&p)
		if err := p.Validate(); err == nil {
			t.Fatalf("accepted invalid manifest %d", i)
		}
	}
	p := Projection{Type: "array", Path: "/items", MaxItems: 2, Items: &Projection{Type: "object", Fields: map[string]Projection{"title": {Type: "string", Path: "/label"}, "date": {Type: "string", Path: "/date", Default: json.RawMessage(`"unknown"`)}}}}
	out, err := project(p, map[string]any{"items": []any{map[string]any{"label": "first"}, map[string]any{"label": "second"}, map[string]any{"label": "third"}}}, false)
	must(t, err)
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "third") || !strings.Contains(string(raw), "unknown") {
		t.Fatal("bounded projection failed")
	}
	if _, err := project(Projection{Type: "number", Path: "/missing"}, map[string]any{}, false); err == nil {
		t.Fatal("missing field silently accepted")
	}
	if !applicationError(map[string]any{"message": "bad"}) {
		t.Fatal("application error not detected")
	}
}

func TestHTTPRecoveryHistoryAcknowledgementAndConnectionRemoval(t *testing.T) {
	s, p, v, a, _ := setupHTTP(t)
	selectConnection(t, s, p, v, a.ID)
	call := httpToolCall(v, "interrupted")
	_, err := s.PrepareCall(t.Context(), call)
	must(t, err)
	must(t, s.RecoverHTTP(t.Context()))
	rows, err := s.HTTPHistory(t.Context(), p, ConnectionRequest{PluginID: v.ID, ID: a.ID})
	must(t, err)
	if len(rows) != 1 || rows[0].State != "unknown" {
		t.Fatal("restart did not preserve unknown request", rows)
	}
	callsBefore, activeBefore, err := s.connectionUsage(t.Context(), a.ID)
	must(t, err)
	if callsBefore != 1 || activeBefore != 0 {
		t.Fatal("restart retained concurrency or erased call count", callsBefore, activeBefore)
	}
	raw, _ := json.Marshal(rows)
	if strings.Contains(string(raw), "quoted") || strings.Contains(string(raw), "secret") {
		t.Fatal("history leaked payload")
	}
	must(t, s.AcknowledgeHTTP(t.Context(), p, AcknowledgeCall{ConnectionID: a.ID, OwnerID: p.OwnerID, ID: call.Execution.OperationID}))
	calls, active, err := s.connectionUsage(t.Context(), a.ID)
	must(t, err)
	if calls != 1 || active != 0 {
		t.Fatal("acknowledgement reset count or retained slot")
	}
	if _, err = s.Execute(t.Context(), call); err == nil {
		t.Fatal("acknowledgement allowed automatic replay")
	}
	must(t, s.RemoveConnection(t.Context(), p, ConnectionRequest{PluginID: v.ID, ID: a.ID, Mutation: core.Mutation{IdempotencyKey: "delete", ExpectedVersion: a.Version}}))
	if _, _, err = s.ConnectionAccess(t.Context(), p, a.ID); core.CodeOf(err) != core.Forbidden {
		t.Fatal("deleted connection still usable", err)
	}
	keys, err := s.options.Credentials.List(t.Context(), p, core.PageRequest{})
	must(t, err)
	if len(keys.Items) != 1 {
		t.Fatal("dedicated credential was not removed")
	}
}

func TestHTTPExplicitCancellationAndGroupRevocation(t *testing.T) {
	for _, reason := range []string{"operation", "group"} {
		t.Run(reason, func(t *testing.T) {
			s, p, v, a, _ := setupHTTP(t)
			selectConnection(t, s, p, v, a.ID)
			started := make(chan struct{})
			done := make(chan error, 1)
			s.options.HTTPClient = func(context.Context, core.Principal, core.Execution, core.ID, string) (*http.Client, error) {
				return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					close(started)
					<-req.Context().Done()
					return nil, req.Context().Err()
				})}, nil
			}
			call := httpToolCall(v, "cancel-test")
			_, err := s.PrepareCall(t.Context(), call)
			must(t, err)
			go func() { _, err := s.Execute(t.Context(), call); done <- err }()
			<-started
			if reason == "operation" {
				s.CancelOperation(p.OwnerID, call.Execution.OperationID)
			} else {
				s.options.CheckTool = func(context.Context, core.Principal, string) error {
					return core.NewError(core.Forbidden, "group revoked")
				}
				s.RevalidateRunning(t.Context())
			}
			if err := <-done; err == nil {
				t.Fatal("request was not canceled")
			}
		})
	}
}

func TestPluginSettingsBridgeRequiresCapabilityAndUpgradeReview(t *testing.T) {
	s, p, v, a, _ := setupHTTP(t)
	selectConnection(t, s, p, v, a.ID)
	q := UIRequest{ID: v.ID, Digest: v.Digest, Kind: "page", Contribution: "settings", Method: "settings.get"}
	if _, err := s.UI(t.Context(), p, q); err != nil {
		t.Fatal(err)
	}
	q.Method = "settings.update"
	q.ExpectedVersion = 1
	q.Values = map[string]any{"backend": string(a.ID), "language": "zh"}
	_, err := s.UI(t.Context(), p, q)
	must(t, err)
	q.Method = "connections.list"
	if _, err = s.UI(t.Context(), p, q); err != nil {
		t.Fatal(err)
	}
	q.Method = "credentials.get"
	if _, err = s.UI(t.Context(), p, q); core.CodeOf(err) != core.Forbidden {
		t.Fatal("unexpected bridge method accepted")
	}
	pkg := httpPackage()
	pkg.Manifest.Version = "2.0.0"
	next, err := s.Install(t.Context(), p, pkg)
	must(t, err)
	v, err = s.Configure(t.Context(), p, Configure{ID: v.ID, Release: "2.0.0", ExpectedVersion: next.Version, Enabled: true, Grants: Requested(pkg.Manifest), Bindings: v.Bindings})
	must(t, err)
	settings, err := s.Settings(t.Context(), p, v.ID, v.Digest)
	must(t, err)
	if !settings.NeedsReview {
		t.Fatal("upgrade restored settings silently")
	}
	if _, err = s.PrepareCall(t.Context(), httpToolCall(v, "after-upgrade")); core.CodeOf(err) != core.FailedPrecondition {
		t.Fatal("upgrade used stale config", err)
	}
	rows, err := s.Connections(t.Context(), p, v.ID, v.Digest)
	must(t, err)
	for _, c := range rows {
		if !c.NeedsReview {
			t.Fatal("upgrade reused connection authorization")
		}
	}
}

func TestCanceledPreparedHTTPCallReleasesConnection(t *testing.T) {
	s, p, v, a, _ := setupHTTP(t)
	selectConnection(t, s, p, v, a.ID)
	call := httpToolCall(v, "cancel-before-dispatch")
	_, err := s.PrepareCall(t.Context(), call)
	must(t, err)
	_, active, err := s.connectionUsage(t.Context(), a.ID)
	must(t, err)
	if active != 1 {
		t.Fatal("prepared call did not hold connection")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = s.Execute(ctx, call); err == nil {
		t.Fatal("canceled call executed")
	}
	calls, active, err := s.connectionUsage(t.Context(), a.ID)
	must(t, err)
	if calls != 1 || active != 0 {
		t.Fatal("canceled prepared call retained connection", calls, active)
	}
	rows, err := s.HTTPHistory(t.Context(), p, ConnectionRequest{PluginID: v.ID, ID: a.ID})
	must(t, err)
	if len(rows) != 1 || rows[0].State != "failed" || rows[0].Failure != "not_dispatched" {
		t.Fatal(rows)
	}
	if _, err = s.Execute(t.Context(), call); err == nil {
		t.Fatal("ended request was replayed")
	}
}
