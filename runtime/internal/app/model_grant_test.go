package app

import (
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/policy"
)

func TestModelGrantDoesNotAuthorizeSiblingModels(t *testing.T) {
	b, err := Build(t.Context(), testConfig(t))
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	ctx := t.Context()
	binding := resource.Binding{Endpoint: "https://provider.example.test/v1/chat/completions", AllowedOrigins: []string{"https://provider.example.test"}, Models: []resource.ModelConfig{{Name: "selected", ContextLimit: 8192, SupportsTools: true}, {Name: "sibling", ContextLimit: 8192, SupportsTools: true}}}
	r, err := b.Services.Resources.Connect(ctx, p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "provider"}, Kind: resource.Model, Name: "Provider", Provider: "openai-compatible", Binding: binding})
	need(t, err)
	entries := modelEntries(r, binding)
	a, err := b.Services.Assistants.Hire(ctx, p, assistant.HireRequest{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}, Mutation: core.Mutation{IdempotencyKey: "hire"}, Name: "Assistant", ModelID: entries[0].id})
	need(t, err)
	need(t, event.NewWriter(b.store).AppendAssistant(ctx, event.AssistantEvent{ID: "wake", OwnerID: p.OwnerID, AssistantID: a.ID, Kind: event.ResumeRequested, CreatedAt: time.Now()}))
	claim, err := scheduler.NewQueue(b.store).Claim(ctx, time.Now())
	need(t, err)
	visible, err := b.Services.Models.List(ctx, claim.Principal)
	need(t, err)
	if len(visible) != 1 || visible[0].ID != entries[0].id {
		t.Fatalf("hiring leaked access to other models: %+v", visible)
	}
	_, err = b.Services.Models.(model.Client).Generate(ctx, model.Request{Execution: core.Execution{Principal: claim.Principal, AssistantID: a.ID, OperationID: "forbidden-call"}, ModelID: entries[1].id, ReservationID: "reservation", Inputs: []model.Input{{Role: model.User, Text: "hello"}}, MaxOutputTokens: 16})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatalf("unselected model reached execution checks: %v", err)
	}
	other, err := b.Services.Resources.Connect(ctx, p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "other-provider"}, Kind: resource.Model, Name: "Other Provider", Provider: "openai-compatible", Binding: binding})
	need(t, err)
	directory, err := b.Services.Resources.Connect(ctx, p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "directory"}, Kind: resource.Storage, Name: "Files", Provider: "local-directory", Binding: resource.Binding{RootPath: t.TempDir()}})
	need(t, err)
	for _, tc := range []struct {
		key                 string
		resourceID, modelID core.ID
	}{
		{"provider-without-model", r.ID, ""},
		{"foreign-model", r.ID, modelEntries(other, binding)[0].id},
		{"unknown-model", r.ID, "missing-model"},
		{"storage-with-model", directory.ID, entries[0].id},
	} {
		_, err := b.Services.Grants.Grant(ctx, p, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: tc.key}, AssistantID: a.ID, ResourceID: tc.resourceID, Actions: []policy.Action{policy.Read}, Scope: policy.Scope{ModelID: tc.modelID}})
		if core.CodeOf(err) != core.InvalidArgument {
			t.Fatalf("invalid scope %s accepted: %v", tc.key, err)
		}
	}
	g, err := b.Services.Grants.Grant(ctx, p, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "view-sibling"}, AssistantID: a.ID, ResourceID: r.ID, Actions: []policy.Action{policy.Read}, Scope: policy.Scope{ModelID: entries[1].id}})
	need(t, err)
	visible, err = b.Services.Models.List(ctx, claim.Principal)
	need(t, err)
	if len(visible) != 2 {
		t.Fatal("selected sibling model not visible")
	}
	check := policy.CheckRequest{Target: core.Object{Kind: "resource", ID: r.ID}, ResourceID: r.ID, ModelID: entries[1].id, Action: policy.Execute}
	if err := b.Services.Grants.(policy.Authorizer).Require(ctx, claim.Principal, check); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("another model's execute grant authorized a read-only model: %v", err)
	}
	clone, err := b.Services.Assistants.Clone(ctx, p, assistant.CloneRequest{Mutation: core.Mutation{IdempotencyKey: "clone"}, SourceID: a.ID, Name: "Copy", GrantIDs: []core.ID{g.ID}})
	need(t, err)
	copies, err := b.Services.Grants.List(ctx, p, policy.Query{AssistantID: clone.ID})
	need(t, err)
	if len(copies.Items) != 1 || copies.Items[0].Scope.ModelID != entries[1].id {
		t.Fatal("clone did not preserve selected model scope")
	}
	need(t, b.Services.Grants.Revoke(ctx, p, policy.RevokeRequest{Mutation: core.Mutation{IdempotencyKey: "revoke-sibling", ExpectedVersion: g.Version}, GrantID: g.ID}))
	visible, err = b.Services.Models.List(ctx, claim.Principal)
	need(t, err)
	if len(visible) != 1 || visible[0].ID != entries[0].id {
		t.Fatal("revocation failed or removed other model access")
	}
}
