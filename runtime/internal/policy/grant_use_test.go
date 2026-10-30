package policy

import (
	"context"
	"errors"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"path/filepath"
	"testing"
	"time"
)

func TestOnceGrantPersistsAndExpiresWithoutRearming(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grants.db")
	now := time.Now().UTC()
	expires := now.Add(time.Hour)
	options := Options{Now: func() time.Time { return now }, ValidatePrincipal: func(context.Context, core.Principal) error { return nil }, Owns: func(context.Context, core.Principal, core.Object) error { return nil }, ResourceActions: func(context.Context, core.Principal, core.ID) ([]string, error) { return []string{"execute"}, nil }}
	store, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	g := Grant{ID: "once", OwnerID: "owner", AssistantID: "assistant", ResourceID: "compute", Actions: []Action{Execute}, Scope: Scope{FullAccess: true, FullAccessScope: &FullAccessScope{Mode: "unrestricted"}, Once: true}, ExpiresAt: &expires, Version: 1}
	if err := store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "security.grants", persistence.Key(g.OwnerID, g.ID), g)
	}); err != nil {
		t.Fatal(err)
	}
	manager, err := NewService(store, options)
	if err != nil {
		t.Fatal(err)
	}
	p := core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "execution"}
	q := CheckRequest{Target: core.Object{Kind: "resource", ID: "compute"}, ResourceID: "compute", Action: Execute, FullAccess: true, OperationID: "operation", ConsumeOnce: true}
	// All record updates roll back if notification fails before effect admission.
	manager.options.GrantUsed = func(context.Context, Grant) error { return errors.New("notification failed") }
	if manager.Require(t.Context(), p, q) == nil {
		t.Fatal("notification failure ignored")
	}
	live, err := persistence.Load[Grant](t.Context(), store, "security.grants", persistence.Key(g.OwnerID, g.ID))
	if err != nil || live.Used != nil {
		t.Fatal("partial consumption", live, err)
	}
	manager.options.GrantUsed = nil
	if err := manager.Require(t.Context(), p, q); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager, err = NewService(store, options)
	if err != nil {
		t.Fatal(err)
	}
	if manager.Require(t.Context(), p, q) == nil {
		t.Fatal("restart allowed repeated admission")
	}
	q.ConsumeOnce = false
	if err := manager.Require(t.Context(), p, q); err != nil {
		t.Fatal("original execution cannot recheck", err)
	}
	q.OperationID = "new-operation"
	if manager.Require(t.Context(), p, q) == nil {
		t.Fatal("another operation used spent grant")
	}
	q.OperationID = "operation"
	now = expires.Add(time.Second)
	if manager.Require(t.Context(), p, q) == nil {
		t.Fatal("expiry was ignored for running operation")
	}
}
