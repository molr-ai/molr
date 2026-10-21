package resource

import (
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"path/filepath"
	"testing"
)

func TestOAuthBindingKeepsModelMembershipAndRegistration(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "bindings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewRepository(db)
	ctx := t.Context()
	r := Resource{ID: "provider", Kind: Model, Provider: "openai-compatible", State: Available, Version: 1}
	b := Binding{ResourceID: r.ID, ChatGPT: &ChatGPTConnection{ClientID: "issued", Subject: "subscriber"}, Models: []ModelConfig{{Name: "before"}}}
	if err = repo.Insert(ctx, r, b); err != nil {
		t.Fatal(err)
	}
	b.Models = []ModelConfig{{Name: "after"}}
	if err = repo.SaveBinding(ctx, b); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.BindingForModel(ctx, "owner", ModelID(r.ID, "before")); core.CodeOf(err) != core.NotFound {
		t.Fatal("stale membership retained", err)
	}
	found, err := repo.BindingForModel(ctx, "owner", ModelID(r.ID, "after"))
	if err != nil || found.ChatGPT.ClientID != "issued" {
		t.Fatal("new membership or registration lost", err)
	}
}
