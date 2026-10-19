package resource

import (
	"context"
	"errors"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"path/filepath"
	"testing"
)

func TestModelIDRemainsStable(t *testing.T) {
	if got := ModelID("provider", "fixture"); got != "model_d311b7c9535eaad26178f578c1b1aece" {
		t.Fatal(got)
	}
	if ModelID("other", "fixture") == ModelID("provider", "fixture") || ModelID("provider", "other") == ModelID("provider", "fixture") {
		t.Fatal("different models share ID")
	}
}

func TestModelBindingIndexTracksLifecycleAndRollback(t *testing.T) {
	db := fixtureDB(t)
	repo := NewRepository(db)
	discovered := []ModelConfig{{Name: "auto-one", Discovered: true}}
	service := NewService(db, Options{Authorizer: fixtureAuthorizer{}, Providers: map[string]Provider{"openai-compatible": {Kind: Model, Validate: func(context.Context, Binding) ([]string, error) { return []string{"read", "execute"}, nil }, Discover: func(context.Context, core.Principal, Binding) ([]ModelConfig, error) { return discovered, nil }}}, CheckVerification: func(context.Context, core.Principal, auth.Verification, string) error { return nil }, OnDeleteProvider: func(context.Context, core.Principal, Resource, Binding) error { return nil }})
	r, err := service.Connect(t.Context(), owner(), ConnectRequest{Mutation: mutation("connect-index", 0), Kind: Model, Name: "Provider", Provider: "openai-compatible", Binding: Binding{Endpoint: "https://one.example.invalid/v1", Models: []ModelConfig{{Name: "manual", ContextLimit: 32768}}}})
	if err != nil {
		t.Fatal(err)
	}
	check := func(ctx context.Context, name, endpoint string, missing bool) {
		t.Helper()
		binding, err := repo.BindingForModel(ctx, owner().OwnerID, ModelID(r.ID, name))
		if missing {
			if core.CodeOf(err) != core.NotFound {
				t.Fatal(name, binding, err)
			}
			return
		}
		if err != nil || binding.ResourceID != r.ID || binding.Endpoint != endpoint {
			t.Fatal(name, binding, err)
		}
	}
	check(t.Context(), "manual", "https://one.example.invalid/v1", false)
	if shared, err := repo.BindingForModel(t.Context(), otherOwner().OwnerID, ModelID(r.ID, "manual")); err != nil || shared.ResourceID != r.ID {
		t.Fatal("shared model binding missing", shared, err)
	}
	r, err = service.ConfigureModel(t.Context(), owner(), ConfigureModelRequest{ID: r.ID, Mutation: mutation("add-model", r.Version), Model: ModelConfig{Name: "added", ContextLimit: 8192}})
	if err != nil {
		t.Fatal(err)
	}
	check(t.Context(), "added", "https://one.example.invalid/v1", false)
	r, err = service.RefreshModels(t.Context(), owner(), RefreshModelsRequest{ID: r.ID, Mutation: mutation("discover-one", r.Version)})
	if err != nil {
		t.Fatal(err)
	}
	check(t.Context(), "auto-one", "https://one.example.invalid/v1", false)
	discovered = []ModelConfig{{Name: "auto-two", Discovered: true}}
	r, err = service.RefreshModels(t.Context(), owner(), RefreshModelsRequest{ID: r.ID, Mutation: mutation("discover-two", r.Version)})
	if err != nil {
		t.Fatal(err)
	}
	check(t.Context(), "auto-one", "", true)
	check(t.Context(), "auto-two", "https://one.example.invalid/v1", false)
	r, err = service.SaveProvider(t.Context(), owner(), SaveProviderRequest{ID: r.ID, Name: "Updated", Endpoint: "https://two.example.invalid/v1", Mutation: mutation("endpoint", r.Version)})
	if err != nil {
		t.Fatal(err)
	}
	check(t.Context(), "auto-two", "", true)
	check(t.Context(), "manual", "https://two.example.invalid/v1", false)
	db.FailNextCommit(errors.New("commit failure"))
	_, err = service.ConfigureModel(t.Context(), owner(), ConfigureModelRequest{ID: r.ID, Mutation: mutation("failed-add", r.Version), Model: ModelConfig{Name: "failed"}})
	if err == nil {
		t.Fatal("commit failure hidden")
	}
	check(t.Context(), "failed", "", true)
	rollback := errors.New("rollback")
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		_, err := service.ConfigureModel(ctx, owner(), ConfigureModelRequest{ID: r.ID, Mutation: mutation("nested-add", r.Version), Model: ModelConfig{Name: "nested"}})
		if err != nil {
			return err
		}
		check(ctx, "nested", "https://two.example.invalid/v1", false)
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	check(t.Context(), "nested", "", true)
	if err := service.DeleteProvider(t.Context(), owner(), DeleteProviderRequest{ID: r.ID, Mutation: mutation("delete-index", r.Version)}); err != nil {
		t.Fatal(err)
	}
	check(t.Context(), "manual", "", true)
	check(t.Context(), "added", "", true)
}

func TestModelBindingIndexSurvivesReopenAndSharedCatalog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bindings.db")
	db, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	r := Resource{ID: "provider", OwnerID: "member", Kind: Model, Provider: "openai-compatible", State: Available, Version: 1}
	b := Binding{ResourceID: r.ID, Endpoint: "https://example.invalid", Models: []ModelConfig{{Name: "fixture"}}}
	if err := NewRepository(db).Insert(t.Context(), r, b); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	found, err := NewRepository(db).BindingForModel(t.Context(), "another-member", ModelID(r.ID, "fixture"))
	if err != nil || found.Endpoint != b.Endpoint {
		t.Fatal(found, err)
	}
}
