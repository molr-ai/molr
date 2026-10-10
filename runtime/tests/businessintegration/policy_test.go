package businessintegration_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/search"
	"molr.ai/runtime/tests/testkit"
)

// These tests compose real authentication, policy and business implementations.
// Only model/provider/Agent execution dependencies are outside their scope.
func TestRealPolicyBusinessCollectionsGrantsAndVerification(t *testing.T) {
	ctx := testkit.Context(t)
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "runtime.db"))
	testkit.RequireNoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	password := core.NewSecret("integration-password-long-enough-123!")
	authenticator, err := auth.NewService(db, auth.Options{MasterKey: make([]byte, 32), Origin: "https://molr.example", ValidateAssistant: func(ctx context.Context, p core.Principal) error {
		a, err := assistant.NewRepository(db).Get(ctx, p.OwnerID, p.SubjectID)
		if err != nil {
			return err
		}
		if a.State != assistant.Active || p.ExecutionID != "execution-a" {
			return core.NewError(core.Forbidden, "assistant execution not active")
		}
		return nil
	}})
	testkit.RequireNoError(t, err)
	_, err = authenticator.Initialize(ctx, auth.InitializeRequest{Username: "owner", Password: password})
	testkit.RequireNoError(t, err)
	login, err := authenticator.Login(ctx, auth.LoginRequest{Username: "owner", Password: password})
	testkit.RequireNoError(t, err)
	if login.Session == nil {
		t.Fatal("expected ordinary owner login")
	}
	owner, err := authenticator.Authenticate(ctx, auth.Authentication{Session: login.Session.Token})
	testkit.RequireNoError(t, err)
	for _, id := range []core.ID{"assistant-a", "assistant-b"} {
		testkit.RequireNoError(t, assistant.NewRepository(db).Insert(ctx, assistant.Assistant{ID: id, OwnerID: owner.OwnerID, Name: string(id), State: assistant.Active, ConversationID: core.ID("conversation-" + string(id)), Version: 1}))
	}
	owns := func(ctx context.Context, p core.Principal, o core.Object) error {
		if o.ID == "" {
			switch o.Kind {
			case "todo", "task", "memory", "resource", "search", "settings":
				return nil
			}
			return core.NewError(core.Forbidden, "unknown collection")
		}
		if o.Kind == "settings" && o.ID == p.OwnerID {
			return nil
		}
		buckets := map[string]string{"assistant": "business.assistants", "todo": "business.todos", "task": "business.tasks", "resource": "business.resources", "settings": "business.exports", "search": "search.jobs"}
		bucket, ok := buckets[o.Kind]
		if !ok {
			return core.NewError(core.Forbidden, "object not owned")
		}
		var row struct {
			OwnerID, AssistantID core.ID
			Scope                string
		}
		if err := db.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return tx.Get(ctx, bucket, persistence.Key(p.OwnerID, o.ID), &row)
		}); err != nil {
			return err
		}
		if p.Kind == core.ActorOwner {
			return nil
		}
		if o.Kind == "resource" || o.Kind == "assistant" && o.ID == p.SubjectID || row.AssistantID == p.SubjectID || o.Kind == "memory" && row.Scope == "shared" {
			return nil
		}
		return core.NewError(core.Forbidden, "assistant binding mismatch")
	}
	authorizer, err := policy.NewService(db, policy.Options{ValidatePrincipal: authenticator.ValidatePrincipal, CheckScope: authenticator.CheckScope, Owns: owns, ResourceActions: func(ctx context.Context, p core.Principal, id core.ID) ([]string, error) {
		r, err := resource.NewRepository(db).Get(ctx, p.OwnerID, id)
		return r.Actions, err
	}})
	testkit.RequireNoError(t, err)
	checkAssistant := func(ctx context.Context, p core.Principal, id core.ID) error {
		return owns(ctx, p, core.Object{Kind: "assistant", ID: id})
	}
	todos := todo.NewService(db, todo.Options{Authorizer: authorizer, CheckAssistant: checkAssistant})
	created, err := todos.Create(ctx, owner, todo.CreateRequest{Mutation: core.Mutation{IdempotencyKey: "todo-create"}, Title: "Protected todo", AssistantID: "assistant-a"})
	testkit.RequireNoError(t, err)
	list, err := todos.List(ctx, owner, todo.Query{})
	testkit.RequireNoError(t, err)
	if len(list.Items) != 1 || list.Items[0].ID != created.ID {
		t.Fatal("real policy rejected or lost collection create/list")
	}
	work := task.NewService(db, task.Options{Authorizer: authorizer, CheckAssistant: checkAssistant, Emit: func(ctx context.Context, p core.Principal, v task.Task, kind string) error {
		return event.NewWriter(db).AppendAssistant(ctx, event.AssistantEvent{ID: core.ID("event-" + string(v.ID)), OwnerID: p.OwnerID, AssistantID: v.AssistantID, TaskID: v.ID, Kind: event.ResumeRequested, CreatedAt: time.Now()})
	}})
	for _, id := range []core.ID{"assistant-a", "assistant-b"} {
		_, err := work.Create(ctx, owner, task.CreateRequest{Mutation: core.Mutation{IdempotencyKey: "task-" + string(id)}, AssistantID: id, Goal: "Budget report"})
		testkit.RequireNoError(t, err)
	}
	agent := core.Principal{OwnerID: owner.OwnerID, SubjectID: "assistant-a", Kind: core.ActorAssistant, ExecutionID: "execution-a"}
	tasks, err := work.List(ctx, agent, task.Query{})
	testkit.RequireNoError(t, err)
	if len(tasks.Items) != 1 || tasks.Items[0].AssistantID != agent.SubjectID {
		t.Fatalf("assistant task collection leaked other assistant: %+v", tasks)
	}
	root := t.TempDir()
	memoryFiles := memory.NewFiles(memory.FileOptions{JournalRoot: filepath.Join(root, "journal"), Roots: func(ctx context.Context, o core.ID, query memory.RootQuery) ([]memory.Root, error) {
		return []memory.Root{{Scope: memory.Assistant, AssistantID: "assistant-a", Directory: filepath.Join(root, string(o), "a")}, {Scope: memory.Assistant, AssistantID: "assistant-b", Directory: filepath.Join(root, string(o), "b")}}, nil
	}})
	memories := memory.NewService(db, memory.Options{Files: memoryFiles, Authorizer: authorizer, CheckAssistant: checkAssistant})
	foreign, err := memories.Create(ctx, owner, memory.CreateRequest{Mutation: memory.Mutation{IdempotencyKey: "private-b"}, Text: "Private memory", Ref: memory.Ref{Scope: memory.Assistant, AssistantID: "assistant-b", Path: ".agents/memory/private-b.md"}})
	testkit.RequireNoError(t, err)
	_, err = memories.Get(ctx, agent, foreign.Ref)
	testkit.RequireCode(t, err, core.Forbidden)
	resources := resource.NewService(db, resource.Options{Authorizer: authorizer, Providers: map[string]resource.Provider{"local": {Kind: resource.Storage, Validate: func(context.Context, resource.Binding) ([]string, error) { return []string{"read"}, nil }}}})
	connected, err := resources.Connect(ctx, owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "connect"}, Kind: resource.Storage, Name: "Budget documents", Provider: "local"})
	testkit.RequireNoError(t, err)
	grant, err := authorizer.Grant(ctx, owner, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "grant"}, AssistantID: agent.SubjectID, ResourceID: connected.ID, Actions: []policy.Action{policy.Read}})
	testkit.RequireNoError(t, err)
	resourceList, err := resources.List(ctx, agent, resource.Query{})
	testkit.RequireNoError(t, err)
	if len(resourceList.Items) != 1 {
		t.Fatal("granted resource missing")
	}
	searcher := search.NewService(db, search.Options{Authorizer: authorizer})
	hits, err := searcher.Search(ctx, agent, search.Query{Text: "budget", Sources: []search.Source{search.Resources}})
	testkit.RequireNoError(t, err)
	if len(hits.Hits.Items) != 1 {
		t.Fatal("authorized search collection missing result")
	}
	testkit.RequireNoError(t, authorizer.Revoke(ctx, owner, policy.RevokeRequest{Mutation: core.Mutation{IdempotencyKey: "revoke", ExpectedVersion: grant.Version}, GrantID: grant.ID}))
	resourceList, err = resources.List(ctx, agent, resource.Query{})
	testkit.RequireNoError(t, err)
	if len(resourceList.Items) != 0 {
		t.Fatal("revoked resource remained listed")
	}
	hits, err = searcher.Search(ctx, agent, search.Query{Text: "budget", Sources: []search.Source{search.Resources}})
	testkit.RequireNoError(t, err)
	if len(hits.Hits.Items) != 0 || hits.Counts[search.Resources] != 0 {
		t.Fatal("revoked resource remained searchable/countable")
	}
	preferences := settings.NewService(db, settings.Options{Authorizer: authorizer, CheckVerification: authenticator.CheckVerification, SaveExport: func(ctx context.Context, p core.Principal, b settings.ExportBundle) (core.ID, error) {
		if _, present := b.Records["auth.accounts"]; present {
			return "", core.NewError(core.Internal, "auth records in export")
		}
		encoded, err := json.Marshal(b)
		if err != nil {
			return "", err
		}
		id := core.ID("export-" + string(b.ID))
		err = db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, "integration.export-bytes", persistence.Key(p.OwnerID, id), encoded)
		})
		return id, err
	}})
	proof, err := authenticator.Reauthenticate(ctx, owner, auth.ReauthenticateRequest{Password: password, Purpose: "export"})
	testkit.RequireNoError(t, err)
	export, err := preferences.Export(ctx, owner, settings.ExportRequest{Mutation: core.Mutation{IdempotencyKey: "export"}, Verification: proof, IncludeMessages: true})
	testkit.RequireNoError(t, err)
	if export.State != "completed" || export.FileID == "" {
		t.Fatal("real verification/export composition failed")
	}
	_, err = preferences.Export(ctx, owner, settings.ExportRequest{Mutation: core.Mutation{IdempotencyKey: "reuse-proof"}, Verification: proof})
	if err == nil {
		t.Fatal("real consumed verification was reused")
	}
	tokenProof, err := authenticator.Reauthenticate(ctx, owner, auth.ReauthenticateRequest{Password: password, Purpose: "token.create"})
	testkit.RequireNoError(t, err)
	token, err := authenticator.CreateToken(ctx, owner, auth.CreateTokenRequest{Name: "read-only", Scopes: []string{"read"}, ExpiresAt: time.Now().Add(time.Hour), Verification: tokenProof})
	testkit.RequireNoError(t, err)
	tokenPrincipal, err := authenticator.Authenticate(ctx, auth.Authentication{Bearer: token.Value})
	testkit.RequireNoError(t, err)
	_, err = todos.Create(ctx, tokenPrincipal, todo.CreateRequest{Mutation: core.Mutation{IdempotencyKey: "forbidden-token-write"}, Title: "Escalation"})
	testkit.RequireCode(t, err, core.Forbidden)
	testkit.RequireNoError(t, authenticator.Logout(ctx, owner))
	_, err = todos.List(ctx, owner, todo.Query{})
	testkit.RequireCode(t, err, core.Unauthenticated)
}
