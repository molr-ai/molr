package app

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/storage/files"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type multiUserFixture struct {
	b                        *Bundle
	admin                    core.Principal
	group                    auth.UserGroup
	model                    core.ID
	alice, bob               core.Principal
	aliceSession, bobSession auth.SessionIssued
}

func newMultiUserFixture(t *testing.T, configure ...func(*config.Config)) multiUserFixture {
	t.Helper()
	ctx := t.Context()
	cfg := testConfig(t)
	for _, f := range configure {
		f(&cfg)
	}
	b, err := Build(ctx, cfg)
	need(t, err)
	t.Cleanup(func() { _ = b.Close() })
	admin, _ := ownerSession(t, b)
	m := b.Services.Auth.(*auth.Manager)
	r, err := b.Services.Resources.Connect(ctx, admin, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "group-provider"}, Kind: resource.Model, Name: "Shared provider", Provider: "openai-compatible", Binding: resource.Binding{Endpoint: "https://models.example/v1", AllowedOrigins: []string{"https://models.example"}, Models: []resource.ModelConfig{{Name: "Allowed model", SupportsTools: true, Pricing: &resource.ModelPricing{Currency: "USD", InputMicrosPerMillion: 1000000}}, {Name: "Forbidden model", SupportsTools: true, Pricing: &resource.ModelPricing{Currency: "USD"}}}}})
	need(t, err)
	binding, err := resource.NewRepository(b.store).Binding(ctx, admin.OwnerID, r.ID)
	need(t, err)
	id := modelEntries(r, binding)[0].id
	g, err := m.SaveGroup(ctx, admin, auth.SaveGroupRequest{Mutation: core.Mutation{IdempotencyKey: "group"}, Group: auth.UserGroup{Name: "Family", Resources: []auth.ResourceReference{{Kind: "model", ID: id, Quota: auth.ResourceQuota{Budget: &core.Money{Currency: "USD", Micros: 1000000}, Period: "month", Timezone: "UTC"}}, {Kind: "tool", ID: "conversation_read", Quota: auth.ResourceQuota{Calls: 2, Period: "month", Timezone: "UTC"}}, {Kind: "tool", ID: "conversation_publish"}}}})
	need(t, err)
	create := func(name string) (core.Principal, auth.SessionIssued) {
		_, err := m.CreateUser(ctx, admin, auth.CreateUserRequest{Username: name, DisplayName: name, Password: core.NewSecret(name + "-password-2026!"), GroupID: g.ID, Mutation: core.Mutation{IdempotencyKey: name}})
		need(t, err)
		login, err := m.Login(ctx, auth.LoginRequest{Username: name, Password: core.NewSecret(name + "-password-2026!"), Client: auth.Client{Address: name}})
		need(t, err)
		p, err := m.Authenticate(ctx, auth.Authentication{Session: login.Session.Token})
		need(t, err)
		return p, *login.Session
	}
	a, as := create("alice")
	bb, bs := create("bob")
	return multiUserFixture{b, admin, g, id, a, bb, as, bs}
}
func (f multiUserFixture) assistant(t *testing.T, p core.Principal, name string) assistant.Assistant {
	t.Helper()
	a, err := f.b.Services.Assistants.Hire(t.Context(), p, assistant.HireRequest{Name: name, ModelID: f.model, Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD", Micros: 100000000}, Period: "daily"}, Mutation: core.Mutation{IdempotencyKey: name}})
	need(t, err)
	return a
}
func (f multiUserFixture) execution(t *testing.T, p core.Principal, a assistant.Assistant, id string) core.Execution {
	t.Helper()
	e := core.Execution{Principal: core.Principal{OwnerID: p.OwnerID, SubjectID: a.ID, Kind: core.ActorAssistant, ExecutionID: core.ID("execution-" + id)}, AssistantID: a.ID, OperationID: core.ID("operation-" + id)}
	err := f.b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "agent.jobs", persistence.Key(p.OwnerID, core.ID(id)), scheduler.Job{State: scheduler.Running, ExecutionID: e.Principal.ExecutionID, Event: event.AssistantEvent{ID: core.ID(id), OwnerID: p.OwnerID, AssistantID: a.ID}})
	})
	need(t, err)
	return e
}

func TestMultiUserPrivateDataAndCatalogPermissions(t *testing.T) {
	f := newMultiUserFixture(t)
	ctx := t.Context()
	a := f.assistant(t, f.alice, "private-alice-assistant")
	_, err := f.b.Services.Conversations.SendUser(ctx, f.alice, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "private-message"}, ConversationID: a.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: "PRIVATE-ALICE-CONTENT"}})
	need(t, err)
	file, err := f.b.Services.Files.Upload(ctx, f.alice, files.UploadRequest{Mutation: core.Mutation{IdempotencyKey: "private-file"}, Name: "PRIVATE-ALICE-FILE.txt", MediaType: "text/plain", Size: 7, Body: strings.NewReader("private")})
	need(t, err)
	for _, p := range []core.Principal{f.bob, f.admin} {
		if _, err := f.b.Services.Assistants.Get(ctx, p, a.ID); err == nil {
			t.Fatal("another user's assistant visible")
		}
		if _, err := f.b.Services.Conversations.Get(ctx, p, a.ConversationID); err == nil {
			t.Fatal("another user's conversation visible")
		}
		if _, err := f.b.Services.Files.Open(ctx, p, file.ID); err == nil {
			t.Fatal("another user's attachment visible")
		}
	}
	models, err := f.b.Services.Models.List(ctx, f.alice)
	need(t, err)
	if len(models) != 1 || models[0].ID != f.model {
		t.Fatalf("group model filter failed: %+v", models)
	}
	_, err = f.b.Services.Resources.Connect(ctx, f.alice, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "forbidden"}, Name: "Forbidden", Kind: resource.Model, Provider: "openai-compatible"})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatalf("resource creation accepted: %v", err)
	}
	all, err := f.b.Services.Resources.List(ctx, f.alice, resource.Query{})
	need(t, err)
	if len(all.Items) != 1 {
		t.Fatalf("shared catalog unavailable: %+v", all)
	}
	_, err = f.b.Services.Resources.ProviderConnection(ctx, f.alice, all.Items[0].ID)
	if core.CodeOf(err) != core.Forbidden {
		t.Fatalf("provider configuration exposed: %v", err)
	}
	stats, err := adminUsage(ctx, f.b.store, f.b.Services.Auth.(*auth.Manager), f.admin)
	need(t, err)
	raw, _ := json.Marshal(stats)
	if strings.Contains(string(raw), "PRIVATE-") || strings.Contains(string(raw), string(a.ID)) {
		t.Fatal("statistics included private data")
	}
	_, err = adminUsage(ctx, f.b.store, f.b.Services.Auth.(*auth.Manager), f.alice)
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("ordinary user viewed global usage")
	}
	_, err = f.b.Services.Assistants.Update(ctx, f.alice, assistant.UpdateRequest{ID: a.ID, Name: "my new name", ModelID: f.model, Budget: a.Budget, Mutation: core.Mutation{IdempotencyKey: "edit", ExpectedVersion: a.Version}})
	need(t, err)
	// Removing the model reference revokes old grants without changing private data.
	f.group.Resources = f.group.Resources[1:]
	_, err = f.b.Services.Auth.(*auth.Manager).SaveGroup(ctx, f.admin, auth.SaveGroupRequest{Group: f.group, Mutation: core.Mutation{IdempotencyKey: "revoke-model", ExpectedVersion: f.group.Version}})
	need(t, err)
	models, err = f.b.Services.Models.List(ctx, f.alice)
	need(t, err)
	if len(models) != 0 {
		t.Fatal("removed reference still listed")
	}
}

func TestGroupModelQuotaIsAtomicAcrossAssistantsAndIndependentAcrossUsers(t *testing.T) {
	f := newMultiUserFixture(t)
	ctx := t.Context()
	a := f.assistant(t, f.alice, "alice-one")
	a2 := f.assistant(t, f.alice, "alice-two")
	b := f.assistant(t, f.bob, "bob-one")
	e1, e2, eb := f.execution(t, f.alice, a, "one"), f.execution(t, f.alice, a2, "two"), f.execution(t, f.bob, b, "bob")
	budget := f.b.Services.Budget.(policy.Budget)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, e := range []core.Execution{e1, e2} {
		wg.Add(1)
		go func(e core.Execution) {
			defer wg.Done()
			_, err := budget.Reserve(ctx, policy.ReserveRequest{Execution: e, ModelID: f.model, Estimate: core.Money{Currency: "USD", Micros: 600000}})
			results <- err
		}(e)
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else if core.CodeOf(err) != core.BudgetExceeded {
			t.Fatal(err)
		}
	}
	if wins != 1 {
		t.Fatalf("group ceiling admitted %d concurrent reservations", wins)
	}
	_, err := budget.Reserve(ctx, policy.ReserveRequest{Execution: eb, ModelID: f.model, Estimate: core.Money{Currency: "USD", Micros: 600000}})
	need(t, err)
	admin := f.assistant(t, f.admin, "admin-assistant")
	ea := f.execution(t, f.admin, admin, "admin")
	_, err = budget.Reserve(ctx, policy.ReserveRequest{Execution: ea, ModelID: f.model, Estimate: core.Money{Currency: "USD", Micros: 2000000}})
	need(t, err)
	ea.OperationID = "admin-over-assistant-limit"
	_, err = budget.Reserve(ctx, policy.ReserveRequest{Execution: ea, ModelID: f.model, Estimate: core.Money{Currency: "USD", Micros: 100000000}})
	if core.CodeOf(err) != core.BudgetExceeded {
		t.Fatalf("administrator bypassed assistant budget: %v", err)
	}
}

func TestOrdinaryUserHTTPManagementIsDenied(t *testing.T) {
	f := newMultiUserFixture(t)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://runtime.example"+path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-Host", "runtime.example")
		r.Header.Set("Origin", "https://runtime.example")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", f.aliceSession.CSRF.Reveal())
		r.AddCookie(&http.Cookie{Name: "__Host-molr_session", Value: f.aliceSession.Token.Reveal()})
		w := httptest.NewRecorder()
		f.b.HTTP.Handler().ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/api/admin/users", "/api/admin/groups", "/api/admin/catalog", "/api/admin/usage"} {
		w := request("GET", path, "")
		if w.Code != 403 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/api/plugins/install", "/api/plugins/configure", "/api/plugins/remove", "/api/sandboxes/connect"} {
		w := request("POST", path, "{}")
		if w.Code != 403 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/api/auth/me", "/api/account", "/api/account/quotas", "/api/settings"} {
		w := request("GET", path, "")
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
}

func TestUserWithoutSandboxCanBuildChatContext(t *testing.T) {
	f := newMultiUserFixture(t)
	a := f.assistant(t, f.alice, "no-sandbox")
	e := f.execution(t, f.alice, a, "context")
	adapters := adapters{store: f.b.store, storageConfig: config.Sandbox{Enabled: true}}
	files := resourceFileTools{adapters: adapters, router: &sandboxRouter{adapters: adapters}}
	docs, err := files.contextSources(t.Context(), agent.ContextRequest{Execution: e})
	need(t, err)
	requests := permissionRequests{store: f.b.store, authorizer: f.b.Services.Grants.(policy.Authorizer)}
	_, err = requests.context(t.Context(), e)
	need(t, err)
	for _, d := range docs {
		if (d.Kind == "skill" && d.Source != "/molr/.agents/skills/molr-docs/SKILL.md") || d.Source == "/work/AGENTS.md" {
			t.Fatal("unassigned sandbox context exposed")
		}
	}
	for _, owner := range []core.Principal{f.alice, f.bob} {
		a := f.assistant(t, owner, "builtin-docs")
		e := f.execution(t, owner, a, "builtin-docs-read")
		sources, err := files.contextSources(t.Context(), agent.ContextRequest{Execution: e})
		need(t, err)
		found := false
		for _, source := range sources {
			if source.Source == "/molr/.agents/skills/molr-docs/SKILL.md" {
				found = true
			}
		}
		if !found {
			t.Fatal("member without sandbox cannot discover documentation")
		}
		raw, err := files.read(t.Context(), e, tools.FileReadRequest{FileTarget: tools.FileTarget{Path: "/molr/.agents/skills/molr-docs/SKILL.md"}})
		need(t, err)
		if !strings.Contains(string(raw), "name: molr-docs") {
			t.Fatal("member cannot read shared product documentation")
		}
	}
}

func TestMemberCanResolveOwnPermissionRequestsWithinGroup(t *testing.T) {
	f := newMultiUserFixture(t)
	ctx := t.Context()
	a := f.assistant(t, f.alice, "approval")
	models, err := f.b.Services.Models.List(ctx, f.alice)
	need(t, err)
	provider := models[0].ResourceID
	s := permissionRequests{store: f.b.store, authorizer: f.b.Services.Grants.(policy.Authorizer), grants: f.b.Services.Grants}
	seed := func(id core.ID) {
		need(t, f.b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, permissionRequestsBucket, persistence.Key(f.alice.OwnerID, id), policy.PermissionRequest{ID: id, OwnerID: f.alice.OwnerID, AssistantID: a.ID, State: "pending", Version: 1})
		}))
	}
	seed("request")
	q := policy.PermissionResolution{RequestID: "request", Decision: "approve", ResourceID: provider, Actions: []policy.Action{policy.Read, policy.Execute}, Scope: policy.Scope{ModelID: f.model}, Mutation: core.Mutation{IdempotencyKey: "approve", ExpectedVersion: 1}}
	for _, p := range []core.Principal{f.bob, f.admin} {
		if _, err := s.Resolve(ctx, p, q); err == nil {
			t.Fatal("foreign account resolved private request")
		}
	}
	result, err := s.Resolve(ctx, f.alice, q)
	need(t, err)
	if result.State != "approved" {
		t.Fatal(result.State)
	}
	seed("forbidden")
	q.RequestID = "forbidden"
	q.Mutation.IdempotencyKey = "forbidden"
	q.Scope.ModelID = "unassigned-model"
	if _, err := s.Resolve(ctx, f.alice, q); core.CodeOf(err) != core.Forbidden {
		t.Fatal("approval bypassed group", err)
	}
	result, err = s.Resolve(ctx, f.alice, policy.PermissionResolution{RequestID: "forbidden", Decision: "reject", Mutation: core.Mutation{IdempotencyKey: "reject", ExpectedVersion: 1}})
	need(t, err)
	if result.State != "rejected" {
		t.Fatal(result.State)
	}
}
