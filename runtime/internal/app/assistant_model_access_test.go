package app

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
)

func TestHireGrantsSelectedProviderAtomically(t *testing.T) {
	b, err := Build(t.Context(), testConfig(t))
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	ctx := t.Context()
	modelID := configureTestModel(t, b, p)
	_, err = b.Services.Resources.Connect(ctx, p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "other-provider"}, Kind: resource.Model, Name: "other", Provider: "openai-compatible", Binding: resource.Binding{Endpoint: "https://other.example.test/v1", AllowedOrigins: []string{"https://other.example.test"}, Models: []resource.ModelConfig{{Name: "other", ContextLimit: 8192}}}})
	need(t, err)
	req := assistant.HireRequest{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}, Mutation: core.Mutation{IdempotencyKey: "hire"}, Name: "Assistant", ModelID: modelID}
	b.store.(*sqlitestore.Store).FailNextCommit(errors.New("injected hire commit failure"))
	if _, err = b.Services.Assistants.Hire(ctx, p, req); err == nil {
		t.Fatal("expected transaction failure")
	}
	rows, err := b.Services.Assistants.List(ctx, p, assistant.Query{})
	need(t, err)
	grants, err := b.Services.Grants.List(ctx, p, policy.Query{})
	need(t, err)
	if len(rows.Items) != 0 || len(grants.Items) != 0 {
		t.Fatal("failed hire left assistant or grants behind")
	}
	a, err := b.Services.Assistants.Hire(ctx, p, req)
	need(t, err)
	replayed, err := b.Services.Assistants.Hire(ctx, p, req)
	need(t, err)
	if replayed.ID != a.ID {
		t.Fatal("retry created another assistant")
	}
	grants, err = b.Services.Grants.List(ctx, p, policy.Query{AssistantID: a.ID})
	need(t, err)
	if len(grants.Items) != 1 {
		t.Fatalf("expected exactly one automatic grant, got %d", len(grants.Items))
	}
	g := grants.Items[0]
	if !reflect.DeepEqual(g.Actions, []policy.Action{policy.Read, policy.Execute}) {
		t.Fatalf("incorrect automatic grant: %+v", g)
	}
	need(t, event.NewWriter(b.store).AppendAssistant(ctx, event.AssistantEvent{ID: "wake", OwnerID: p.OwnerID, AssistantID: a.ID, Kind: event.ResumeRequested, CreatedAt: time.Now()}))
	claim, err := scheduler.NewQueue(b.store).Claim(ctx, time.Now())
	need(t, err)
	models, err := b.Services.Models.List(ctx, claim.Principal)
	need(t, err)
	if len(models) != 1 || models[0].ID != modelID || models[0].ResourceID != g.ResourceID {
		t.Fatalf("model access must be limited to selected provider: %+v", models)
	}
	authorizer := b.Services.Grants.(policy.Authorizer)
	check := policy.CheckRequest{Target: core.Object{Kind: "resource", ID: g.ResourceID}, ResourceID: g.ResourceID, Action: policy.Execute, Destination: "https://provider.example.test/v1/chat/completions"}
	need(t, authorizer.Require(ctx, claim.Principal, check))
	check.Destination, check.Action = "https://provider.example.test/v1", policy.Write
	if err = authorizer.Require(ctx, claim.Principal, check); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("grant allowed write access: %v", err)
	}
	need(t, b.Services.Grants.Revoke(ctx, p, policy.RevokeRequest{Mutation: core.Mutation{IdempotencyKey: "revoke", ExpectedVersion: g.Version}, GrantID: g.ID}))
	_, err = b.Services.Assistants.Hire(ctx, p, req)
	need(t, err)
	_, err = b.Services.Assistants.Update(ctx, p, assistant.UpdateRequest{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}, Mutation: core.Mutation{IdempotencyKey: "rename", ExpectedVersion: a.Version}, ID: a.ID, Name: "Renamed", ModelID: modelID})
	need(t, err)
	models, err = b.Services.Models.List(ctx, claim.Principal)
	need(t, err)
	if len(models) != 0 {
		t.Fatal("retry or rename silently restored revoked model access")
	}
}

func TestModelGrantFollowsProviderConnection(t *testing.T) {
	b, err := Build(t.Context(), testConfig(t))
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	ctx := t.Context()
	modelID := configureTestModel(t, b, p)
	a, err := b.Services.Assistants.Hire(ctx, p, assistant.HireRequest{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}, Mutation: core.Mutation{IdempotencyKey: "hire"}, Name: "Assistant", ModelID: modelID})
	need(t, err)
	grants, err := b.Services.Grants.List(ctx, p, policy.Query{AssistantID: a.ID})
	need(t, err)
	g := grants.Items[0]
	r, err := b.Services.Resources.Get(ctx, p, g.ResourceID)
	need(t, err)
	const endpoint = "https://replacement.example.test/v1/chat/completions"
	_, err = b.Services.Resources.SaveProvider(ctx, p, resource.SaveProviderRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "change-endpoint", ExpectedVersion: r.Version}, Name: r.Name, Endpoint: endpoint})
	need(t, err)
	need(t, event.NewWriter(b.store).AppendAssistant(ctx, event.AssistantEvent{ID: "wake", OwnerID: p.OwnerID, AssistantID: a.ID, Kind: event.ResumeRequested, CreatedAt: time.Now()}))
	claim, err := scheduler.NewQueue(b.store).Claim(ctx, time.Now())
	need(t, err)
	models, err := b.Services.Models.List(ctx, claim.Principal)
	need(t, err)
	if len(models) != 1 || models[0].ID != modelID {
		t.Fatal("provider address change required a second grant")
	}
	ad := adapters{store: b.store, policy: b.Services.Grants.(policy.Authorizer)}
	e := core.Execution{Principal: claim.Principal, AssistantID: a.ID, OperationID: "call"}
	need(t, ad.destination(ctx, e, r.ID, endpoint))
	for _, target := range []string{"https://provider.example.test/v1/chat/completions", "https://arbitrary.example.test/v1/chat/completions"} {
		if err := ad.destination(ctx, e, r.ID, target); core.CodeOf(err) != core.Forbidden {
			t.Fatalf("request to unconfigured destination allowed: %s: %v", target, err)
		}
	}
	current, err := b.Services.Grants.List(ctx, p, policy.Query{AssistantID: a.ID})
	need(t, err)
	if len(current.Items) != 1 || !reflect.DeepEqual(current.Items[0], g) {
		t.Fatal("changing provider address should not rewrite assistant grants")
	}
}
