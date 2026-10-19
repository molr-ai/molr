package app

import (
	"context"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/modelcatalog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/storage/files"
)

func TestBuildAuthorizationUsesLiveJobAndPeerBinding(t *testing.T) {
	b, err := Build(t.Context(), testConfig(t))
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	ctx := t.Context()
	modelID := configureTestModel(t, b, p)
	hire := func(key string) assistant.Assistant {
		v, err := b.Services.Assistants.Hire(ctx, p, assistant.HireRequest{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}, Mutation: core.Mutation{IdempotencyKey: key}, Name: key, ModelID: modelID})
		need(t, err)
		return v
	}
	first, second := hire("first"), hire("second")
	need(t, event.NewWriter(b.store).AppendAssistant(ctx, event.AssistantEvent{ID: "wake", OwnerID: p.OwnerID, AssistantID: first.ID, Kind: event.ResumeRequested, CreatedAt: time.Now()}))
	claim, err := scheduler.NewQueue(b.store).Claim(ctx, time.Now())
	need(t, err)
	ap := claim.Principal
	_, err = b.Services.Conversations.Get(ctx, ap, first.ConversationID)
	need(t, err)
	if _, err = b.Services.Conversations.Get(ctx, ap, second.ConversationID); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("peer isolation: %v", err)
	}
	private, err := b.Services.Memories.Create(ctx, p, memory.CreateRequest{Mutation: memory.Mutation{IdempotencyKey: "private"}, Text: "private second assistant memory", Ref: memory.Ref{Scope: memory.Assistant, AssistantID: second.ID, Path: ".agents/memory/private.md"}})
	need(t, err)
	if _, err = b.Services.Memories.Get(ctx, ap, private.Ref); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("private memory isolation: %v", err)
	}
	f, err := b.Services.Files.Upload(ctx, p, files.UploadRequest{Mutation: core.Mutation{IdempotencyKey: "upload"}, Name: "proof.txt", Size: 5, Body: strings.NewReader("proof")})
	need(t, err)
	if _, err = b.Services.Files.Get(ctx, ap, f.ID); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("unattached file leaked: %v", err)
	}
	_, err = b.Services.Conversations.SendUser(ctx, p, conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "attachment"}, ConversationID: first.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: "attachment", AttachmentIDs: []core.ID{f.ID}}})
	need(t, err)
	_, err = b.Services.Files.Get(ctx, ap, f.ID)
	need(t, err)
	forged := ap
	forged.ExecutionID = "forged"
	if _, err = b.Services.Conversations.Get(ctx, forged, first.ConversationID); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("forged execution: %v", err)
	}
	need(t, scheduler.NewQueue(b.store).Cancel(ctx, p.OwnerID, claim.Job.Event.ID))
	if _, err = b.Services.Conversations.Get(ctx, ap, first.ConversationID); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("canceled identity remained live: %v", err)
	}
}
func TestBuildResourceCannotReadRuntimeOrEscapeDirectory(t *testing.T) {
	cfg := testConfig(t)
	b, err := Build(t.Context(), cfg)
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	ctx := t.Context()
	_, err = b.Services.Resources.Connect(ctx, p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "insecure-model"}, Kind: resource.Model, Name: "bad-public-http", Provider: "openai-compatible", Binding: resource.Binding{Endpoint: "http://93.184.216.34/v1", AllowedOrigins: []string{"http://93.184.216.34"}, Models: []resource.ModelConfig{{Name: "test", ContextLimit: 8192}}}})
	if core.CodeOf(err) != core.InvalidArgument {
		t.Fatalf("public model accepted plaintext: %v", err)
	}
	_, err = b.Services.Resources.Connect(ctx, p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "private-root"}, Kind: resource.Storage, Name: "bad", Provider: "local-directory", Binding: resource.Binding{RootPath: cfg.DataDir}})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatalf("runtime exposed: %v", err)
	}
	modelID := configureTestModel(t, b, p)
	a, err := b.Services.Assistants.Hire(ctx, p, assistant.HireRequest{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}, Mutation: core.Mutation{IdempotencyKey: "hire"}, Name: "assistant", ModelID: modelID})
	need(t, err)
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	need(t, os.WriteFile(outside, []byte("outside-secret"), 0600))
	need(t, os.Symlink(outside, filepath.Join(root, "escape")))
	need(t, os.WriteFile(filepath.Join(root, "ok"), []byte("permitted"), 0600))
	r, err := b.Services.Resources.Connect(ctx, p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "directory"}, Kind: resource.Storage, Name: "directory", Provider: "local-directory", Binding: resource.Binding{RootPath: root}})
	need(t, err)
	_, err = b.Services.Grants.Grant(ctx, p, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "grant"}, AssistantID: a.ID, ResourceID: r.ID, Actions: []policy.Action{policy.Read}})
	need(t, err)
	need(t, event.NewWriter(b.store).AppendAssistant(ctx, event.AssistantEvent{ID: "wake", OwnerID: p.OwnerID, AssistantID: a.ID, Kind: event.ResumeRequested, CreatedAt: time.Now()}))
	claim, err := scheduler.NewQueue(b.store).Claim(ctx, time.Now())
	need(t, err)
	e := core.Execution{Principal: claim.Principal, AssistantID: a.ID, OperationID: "read"}
	// Reuse the exact production adapters and authorizer, not a test bypass.
	identity := b.Services.Auth.(interface {
		ValidatePrincipal(context.Context, core.Principal) error
		CheckScope(context.Context, core.Principal, string) error
	})
	ac := access{b.store}
	permission, err := policy.NewService(b.store, policy.Options{ValidatePrincipal: identity.ValidatePrincipal, CheckScope: identity.CheckScope, Owns: ac.owns, ResourceActions: ac.resourceActions})
	need(t, err)
	ad := adapters{store: b.store, policy: permission, dataDir: cfg.DataDir}
	data, err := (&resourceFileTools{adapters: ad}).read(ctx, e, tools.FileReadRequest{FileTarget: tools.FileTarget{ResourceID: r.ID, Path: "ok"}})
	need(t, err)
	if !strings.Contains(string(data), "permitted") {
		t.Fatal(string(data))
	}
	if _, err = (&resourceFileTools{adapters: ad}).read(ctx, e, tools.FileReadRequest{FileTarget: tools.FileTarget{ResourceID: r.ID, Path: "escape"}}); err == nil {
		t.Fatal("symlink escaped approved root")
	}
	if _, err = (&resourceFileTools{adapters: ad}).read(ctx, e, tools.FileReadRequest{FileTarget: tools.FileTarget{ResourceID: r.ID, Path: "../secret"}}); err == nil {
		t.Fatal("traversal escaped root")
	}
}
func TestExistingDatabaseNeverGetsReplacementMasterKey(t *testing.T) {
	cfg := testConfig(t)
	b, err := Build(t.Context(), cfg)
	need(t, err)
	ownerSession(t, b)
	need(t, b.Close())
	need(t, os.Remove(filepath.Join(cfg.DataDir, "keys", "master.key")))
	if _, err = Build(t.Context(), cfg); core.CodeOf(err) != core.FailedPrecondition {
		t.Fatalf("missing key silently replaced: %v", err)
	}
}

func configureTestModel(t *testing.T, b *Bundle, p core.Principal) core.ID {
	t.Helper()
	r, err := b.Services.Resources.Connect(t.Context(), p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "test-model"}, Kind: resource.Model, Name: "test", Provider: "openai-compatible", Binding: resource.Binding{Endpoint: "https://provider.example.test/v1/chat/completions", AllowedOrigins: []string{"https://provider.example.test"}, Models: []resource.ModelConfig{{Name: "test", ContextLimit: 8192, SupportsTools: true, Pricing: &resource.ModelPricing{Currency: "USD"}}}}})
	need(t, err)
	return modelcatalog.Entries(r, resource.Binding{Models: []resource.ModelConfig{{Name: "test"}}})[0].ID
}
