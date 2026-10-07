package app

import (
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/policy"
)

func TestGrantOnlySupportsResourceActions(t *testing.T) {
	b, err := Build(t.Context(), testConfig(t))
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	ctx := t.Context()
	modelID := configureTestModel(t, b, p)
	a, err := b.Services.Assistants.Hire(ctx, p, assistant.HireRequest{Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}, Mutation: core.Mutation{IdempotencyKey: "hire"}, Name: "Assistant", ModelID: modelID})
	need(t, err)
	models, err := b.Services.Models.List(ctx, p)
	need(t, err)
	directory, err := b.Services.Resources.Connect(ctx, p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "directory"}, Kind: resource.Storage, Name: "Files", Provider: "local-directory", Binding: resource.Binding{RootPath: t.TempDir()}})
	need(t, err)
	for _, test := range []struct {
		name     string
		resource core.ID
		action   policy.Action
	}{
		{"model-write", models[0].ResourceID, policy.Write},
		{"model-manage", models[0].ResourceID, policy.Manage},
		{"storage-execute", directory.ID, policy.Execute},
		{"storage-manage", directory.ID, policy.Manage},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := b.Services.Grants.Grant(ctx, p, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: test.name}, AssistantID: a.ID, ResourceID: test.resource, Actions: []policy.Action{policy.Read, test.action}})
			if core.CodeOf(err) != core.InvalidArgument {
				t.Fatalf("unsupported action accepted: %v", err)
			}
		})
	}
	grants, err := b.Services.Grants.List(ctx, p, policy.Query{AssistantID: a.ID})
	need(t, err)
	if len(grants.Items) != 1 {
		t.Fatal("rejected requests persisted grants")
	}
	_, err = b.Services.Grants.Grant(ctx, p, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "storage-read-write"}, AssistantID: a.ID, ResourceID: directory.ID, Actions: []policy.Action{policy.Read, policy.Write}})
	need(t, err)
	need(t, event.NewWriter(b.store).AppendAssistant(ctx, event.AssistantEvent{ID: "wake", OwnerID: p.OwnerID, AssistantID: a.ID, Kind: event.ResumeRequested, CreatedAt: time.Now()}))
	claim, err := scheduler.NewQueue(b.store).Claim(ctx, time.Now())
	need(t, err)
	check := policy.CheckRequest{Target: core.Object{Kind: "resource", ID: directory.ID}, ResourceID: directory.ID, Action: policy.Write}
	authorizer := b.Services.Grants.(policy.Authorizer)
	need(t, authorizer.Require(ctx, claim.Principal, check))
	// Even existing grants cannot exceed a provider's current capabilities.
	directory.Actions = []string{"read"}
	directory.Version++
	need(t, resource.NewRepository(b.store).Save(ctx, directory, directory.Version-1))
	if err = authorizer.Require(ctx, claim.Principal, check); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("capability removal left write access active: %v", err)
	}
	check.Action = policy.Read
	need(t, authorizer.Require(ctx, claim.Principal, check))
}
