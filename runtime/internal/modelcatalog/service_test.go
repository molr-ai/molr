package modelcatalog_test

import (
	"context"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/modelcatalog"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"path/filepath"
	"testing"
)

func TestModelCatalogResolutionAndRevocation(t *testing.T) {
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	r := resource.Resource{ID: "provider", OwnerID: "admin", Kind: resource.Model, Provider: "openai-compatible", State: resource.Available, Version: 1}
	b := resource.Binding{ResourceID: r.ID, Endpoint: "https://example.invalid", Models: []resource.ModelConfig{{Name: "one", ProviderContextLimit: 128000, ContextLimit: 64000, SupportsTools: true}, {Name: "two"}}}
	if err := resource.NewRepository(store).Insert(t.Context(), r, b); err != nil {
		t.Fatal(err)
	}
	entries := modelcatalog.Entries(r, b)
	group := auth.UserGroup{ID: "members", Resources: []auth.ResourceReference{{Kind: "model", ID: entries[0].ID}}}
	save := func() {
		t.Helper()
		if err := store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, "auth.groups", "members", group)
		}); err != nil {
			t.Fatal(err)
		}
	}
	save()
	s := modelcatalog.Service{Store: store, Group: func(ctx context.Context, store persistence.Store, owner core.ID) (auth.UserGroup, error) {
		return persistence.Load[auth.UserGroup](ctx, store, "auth.groups", "members")
	}}
	p := core.Principal{OwnerID: "member", SubjectID: "member", Kind: core.ActorOwner, SessionID: "session"}
	got, err := s.Resolve(t.Context(), p, entries[0].ID)
	if err != nil || got.Model.ContextWindow() != 64000 || got.Model.ContextLimitSource != "manual" {
		t.Fatal(got, err)
	}
	if _, err := s.Resolve(t.Context(), p, entries[1].ID); core.CodeOf(err) != core.Forbidden {
		t.Fatal(err)
	}
	rows, err := s.List(t.Context(), p)
	if err != nil || len(rows) != 1 || rows[0].ID != got.Model.ID {
		t.Fatal(rows, err)
	}
	group.Resources = nil
	save()
	if _, err := s.Resolve(t.Context(), p, entries[0].ID); core.CodeOf(err) != core.Forbidden {
		t.Fatal("revocation ignored", err)
	}
	rows, err = s.List(t.Context(), p)
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}
