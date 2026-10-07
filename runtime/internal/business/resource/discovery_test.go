package resource

import (
	"context"
	"testing"

	"molr.ai/runtime/internal/core"
)

func TestConnectionTestIsOwnerOnlyAndDoesNotCreateResource(t *testing.T) {
	db := fixtureDB(t)
	calls := 0
	s := NewService(db, Options{Authorizer: fixtureAuthorizer{}, Providers: map[string]Provider{"test": {
		Kind: Model, Validate: func(context.Context, Binding) ([]string, error) { return nil, nil },
		Test: func(context.Context, core.Principal, TestConnectionRequest) (ConnectionTest, error) {
			calls++
			return ConnectionTest{ModelCount: 3}, nil
		},
	}}})
	req := TestConnectionRequest{Provider: "test"}
	result, err := s.TestConnection(t.Context(), owner(), req)
	if err != nil || result.ModelCount != 3 {
		t.Fatalf("test result: %#v %v", result, err)
	}
	rows, err := s.List(t.Context(), owner(), Query{})
	if err != nil || len(rows.Items) != 0 {
		t.Fatal("connection test saved a resource")
	}
	_, err = s.TestConnection(t.Context(), agent(), req)
	wantDenied(t, err)
	if calls != 1 {
		t.Fatal("unauthorized probe dispatched")
	}
}

func TestRefreshModelsPreservesOverridesAndMutationContracts(t *testing.T) {
	db := fixtureDB(t)
	s := NewService(db, Options{Authorizer: fixtureAuthorizer{}, Providers: map[string]Provider{"test": {
		Kind: Model, Validate: func(context.Context, Binding) ([]string, error) { return []string{"read", "execute"}, nil },
		Discover: func(context.Context, core.Principal, Binding) ([]ModelConfig, error) {
			return []ModelConfig{{Name: "manual", Discovered: true}, {Name: "auto", Discovered: true}}, nil
		},
	}}})
	r, err := s.Connect(t.Context(), owner(), ConnectRequest{Mutation: mutation("connect", 0), Kind: Model, Provider: "test", Name: "provider", Binding: Binding{Models: []ModelConfig{{Name: "manual", ContextLimit: 8192, Pricing: &ModelPricing{Currency: "USD", InputMicrosPerMillion: 25}}}}})
	if err != nil {
		t.Fatal(err)
	}
	request := RefreshModelsRequest{ID: r.ID, Mutation: mutation("refresh", r.Version)}
	updated, err := s.RefreshModels(t.Context(), owner(), request)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewRepository(db).Binding(t.Context(), owner().OwnerID, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Models) != 2 || b.Models[0].Discovered || b.Models[0].ContextLimit != 8192 || b.Models[0].Pricing.InputMicrosPerMillion != 25 || b.Models[1].Pricing != nil {
		t.Fatalf("manual model overwritten: %#v", b.Models)
	}
	replay, err := s.RefreshModels(t.Context(), owner(), request)
	if err != nil || replay.Version != updated.Version {
		t.Fatalf("refresh replay: %v %#v", err, replay)
	}
	_, err = s.RefreshModels(t.Context(), owner(), RefreshModelsRequest{ID: r.ID, Mutation: mutation("stale", r.Version)})
	wantCode(t, err, core.Conflict)
	_, err = s.RefreshModels(t.Context(), otherOwner(), request)
	wantDenied(t, err)
	_, err = s.RefreshModels(t.Context(), agent(), request)
	wantDenied(t, err)
}

func TestConfigureModelPersistsOverrideAndRejectsStaleOrUnauthorizedChanges(t *testing.T) {
	db := fixtureDB(t)
	s := NewService(db, Options{Authorizer: fixtureAuthorizer{}, Providers: map[string]Provider{"test": {
		Kind: Model, Validate: func(context.Context, Binding) ([]string, error) { return []string{"read", "execute"}, nil },
		Discover: func(context.Context, core.Principal, Binding) ([]ModelConfig, error) {
			return []ModelConfig{{Name: "auto", Discovered: true}}, nil
		},
	}}})
	r, err := s.Connect(t.Context(), owner(), ConnectRequest{Mutation: mutation("connect", 0), Kind: Model, Provider: "test", Name: "provider"})
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.RefreshModels(t.Context(), owner(), RefreshModelsRequest{ID: r.ID, Mutation: mutation("discover", r.Version)})
	if err != nil {
		t.Fatal(err)
	}
	req := ConfigureModelRequest{ID: r.ID, Mutation: mutation("configure", r.Version), Model: ModelConfig{Name: "auto", ContextLimit: 8192, SupportsTools: true, Pricing: &ModelPricing{Currency: "USD"}}}
	updated, err := s.ConfigureModel(t.Context(), owner(), req)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.ConfigureModel(t.Context(), owner(), req)
	if err != nil || replay.Version != updated.Version {
		t.Fatalf("configure replay: %v %#v", err, replay)
	}
	_, err = s.ConfigureModel(t.Context(), otherOwner(), req)
	wantDenied(t, err)
	_, err = s.ConfigureModel(t.Context(), agent(), req)
	wantDenied(t, err)
	req.Mutation = mutation("stale", r.Version)
	_, err = s.ConfigureModel(t.Context(), owner(), req)
	wantCode(t, err, core.Conflict)
	_, err = s.RefreshModels(t.Context(), owner(), RefreshModelsRequest{ID: r.ID, Mutation: mutation("refresh", updated.Version)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewRepository(db).Binding(t.Context(), owner().OwnerID, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Models) != 1 || b.Models[0].Discovered || b.Models[0].ContextLimit != 8192 || b.Models[0].Pricing == nil {
		t.Fatalf("configured override lost after discovery: %#v", b.Models)
	}
}
