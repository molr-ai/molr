package app

import (
	"context"
	"encoding/json"
	"errors"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/catalog"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestDefaultResourcesInitializeAtomicallyOnce(t *testing.T) {
	t.Parallel()
	t.Run("deleted-defaults", func(t *testing.T) { testDefaultResourcesInitializeOnce(t, false) })
	t.Run("new-network-policy", func(t *testing.T) { testDefaultResourcesInitializeOnce(t, true) })
}

func testDefaultResourcesInitializeOnce(t *testing.T, addNetworkOnRestart bool) {
	t.Helper()
	cfg := testConfig(t)
	cfg.Sandbox, _ = config.SandboxPreset("permissive")
	networkPresets := cfg.Sandbox.NetworkPresets
	if len(networkPresets) == 0 {
		t.Fatal("expected platform network presets")
	}
	if addNetworkOnRestart {
		// Reserve a supported preset so restart introduces a new policy on every platform.
		cfg.Sandbox.NetworkPresets = networkPresets[:len(networkPresets)-1]
	}
	b, err := Build(t.Context(), cfg)
	need(t, err)
	req := auth.InitializeRequest{Username: "owner", Password: core.NewSecret("resource-initialization-password-2026!")}
	b.store.(*sqlitestore.Store).FailNextCommit(errors.New("initialization failure"))
	_, err = b.InitializeSetup(t.Context(), req)
	if err == nil {
		t.Fatal("expected initialization failure")
	}
	initialized, err := b.Bootstrap.Initialized(t.Context())
	need(t, err)
	if initialized {
		t.Fatal("partial initialization")
	}
	rows, err := catalog.Select[resource.Resource](t.Context(), b.store, "business.resources", persistence.Query{})
	need(t, err)
	if len(rows) != 0 {
		t.Fatal("partial default resources")
	}
	account, err := b.InitializeSetup(t.Context(), req)
	need(t, err)
	login, err := b.Services.Auth.Login(t.Context(), auth.LoginRequest{Username: "owner", Password: req.Password, Client: auth.Client{Address: "127.0.0.1"}})
	need(t, err)
	p, err := b.Services.Auth.Authenticate(t.Context(), auth.Authentication{Session: login.Session.Token})
	need(t, err)
	if p.OwnerID != account.ID {
		t.Fatal("wrong initialization owner")
	}
	rows, err = catalog.Select[resource.Resource](t.Context(), b.store, "business.resources", persistence.Query{})
	need(t, err)
	expected := 2 + len(cfg.Sandbox.NetworkTargets(cfg.OwnListenPort()))
	if len(rows) != expected {
		t.Fatalf("defaults=%d want %d", len(rows), expected)
	}
	for _, r := range rows {
		if r.Provider == "managed-workspace" {
			continue
		}
		need(t, b.Services.Resources.Disconnect(t.Context(), p, resource.DisconnectRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "disconnect-" + string(r.ID), ExpectedVersion: r.Version}}))
		need(t, b.Services.Resources.Delete(t.Context(), p, resource.DeleteRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "delete-" + string(r.ID), ExpectedVersion: r.Version + 1}}))
	}
	need(t, b.Close())
	cfg.Sandbox.NetworkPresets = networkPresets
	next, err := Build(t.Context(), cfg)
	need(t, err)
	defer next.Close()
	need(t, next.refreshStartupResources(t.Context()))
	remaining, err := catalog.Select[resource.Resource](t.Context(), next.store, "business.resources", persistence.Query{})
	need(t, err)
	if len(remaining) != 1 || remaining[0].Provider != "managed-workspace" {
		t.Fatal("restart recreated resources", remaining)
	}
	_, err = next.Bootstrap.Initialize(t.Context(), req)
	if core.CodeOf(err) != core.Conflict {
		t.Fatal("reinitialized existing instance", err)
	}
}

func TestResourceReconnectPreservesGrantsAndDirectory(t *testing.T) {
	t.Parallel()
	f := newResourceAccessFixture(t, func(cfg *config.Config, _ string) { cfg.Sandbox.Enabled = false })
	expires := time.Now().Add(time.Hour).UTC()
	original := f.grant(t, f.storage.ID, "files", []policy.Action{policy.Read}, policy.Scope{}, &expires)
	need(t, f.b.Services.Resources.Disconnect(t.Context(), f.owner, resource.DisconnectRequest{ID: f.storage.ID, Mutation: core.Mutation{IdempotencyKey: "disconnect-files", ExpectedVersion: f.storage.Version}}))
	raw, _ := json.Marshal(map[string]any{"ID": "local"})
	details, err := f.b.remote.sandboxResourceDetails(t.Context(), f.owner, raw)
	need(t, err)
	found := false
	for _, d := range details.([]sandboxResourceDetail) {
		if d.ID == f.storage.ID {
			found = d.HasActiveGrants
		}
	}
	if !found {
		t.Fatal("missing deletion guard")
	}
	err = f.b.Services.Resources.Delete(t.Context(), f.owner, resource.DeleteRequest{ID: f.storage.ID, Mutation: core.Mutation{IdempotencyKey: "blocked-delete", ExpectedVersion: f.storage.Version + 1}})
	if core.ReasonOf(err) != "resource_has_grants" {
		t.Fatal(err)
	}
	req := resource.ReconnectRequest{ID: f.storage.ID, Mutation: core.Mutation{IdempotencyKey: "reconnect-files", ExpectedVersion: f.storage.Version + 1}}
	// Offline/changed directories cannot be reconnected; the old row survives.
	moved := f.root + "-temporarily-moved"
	need(t, os.Rename(f.root, moved))
	_, err = f.b.Services.Resources.Reconnect(t.Context(), f.owner, req)
	if err == nil {
		t.Fatal("reconnected missing directory")
	}
	need(t, os.Rename(moved, f.root))
	r, err := f.b.Services.Resources.Reconnect(t.Context(), f.owner, req)
	need(t, err)
	grants, err := policy.NewRepository(f.b.store).Grants(t.Context(), f.owner.OwnerID, policy.Query{ResourceID: r.ID})
	need(t, err)
	if len(grants.Items) != 1 || !reflect.DeepEqual(grants.Items[0], original) {
		t.Fatal("reconnection changed grants", grants.Items, original)
	}
	need(t, f.b.Services.Resources.Disconnect(t.Context(), f.owner, resource.DisconnectRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "disconnect-again", ExpectedVersion: r.Version}}))
	need(t, f.b.Services.Grants.Revoke(t.Context(), f.owner, policy.RevokeRequest{GrantID: original.ID, Mutation: core.Mutation{IdempotencyKey: "revoke", ExpectedVersion: original.Version}}))
	need(t, f.b.Services.Resources.Delete(t.Context(), f.owner, resource.DeleteRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "delete", ExpectedVersion: r.Version + 1}}))
	data, err := os.ReadFile(filepath.Join(f.root, "read", "data"))
	need(t, err)
	if string(data) != "original" {
		t.Fatal("resource deletion changed files")
	}
	grants, err = policy.NewRepository(f.b.store).Grants(t.Context(), f.owner.OwnerID, policy.Query{ResourceID: r.ID})
	need(t, err)
	if len(grants.Items) != 1 || grants.Items[0].RevokedAt == nil {
		t.Fatal("history removed")
	}
}

func TestResourceRefreshSkipsDisconnectedAndRecoversAvailability(t *testing.T) {
	t.Parallel()
	f := newResourceAccessFixture(t, func(cfg *config.Config, _ string) { cfg.Sandbox.Enabled = false })
	p := f.owner
	need(t, f.b.Services.Resources.Disconnect(t.Context(), p, resource.DisconnectRequest{ID: f.storage.ID, Mutation: core.Mutation{IdempotencyKey: "disconnect", ExpectedVersion: f.storage.Version}}))
	moved := f.root + "-moved"
	need(t, os.Rename(f.root, moved))
	defer os.Rename(moved, f.root)
	watch, err := f.b.store.Watch(persistence.RecordSignal("business.resources", persistence.Key(catalog.Scope, f.storage.ID)))
	need(t, err)
	defer watch.Close()
	for range 2 {
		need(t, f.b.refreshStartupResources(t.Context()))
	}
	select {
	case <-watch.Changes():
		t.Fatal("disconnected resource was refreshed")
	default:
	}
	row, err := f.b.Services.Resources.Get(t.Context(), p, f.storage.ID)
	need(t, err)
	if row.State != resource.Disconnected || row.Version != f.storage.Version+1 {
		t.Fatal(row)
	}
	need(t, os.Rename(moved, f.root))
	row, err = f.b.Services.Resources.Reconnect(t.Context(), p, resource.ReconnectRequest{ID: row.ID, Mutation: core.Mutation{IdempotencyKey: "reconnect", ExpectedVersion: row.Version}})
	need(t, err)
	need(t, os.Rename(f.root, moved))
	need(t, f.b.refreshStartupResources(t.Context()))
	row, err = f.b.Services.Resources.Get(t.Context(), p, row.ID)
	need(t, err)
	if row.State != resource.Offline {
		t.Fatal(row)
	}
	need(t, os.Rename(moved, f.root))
	need(t, f.b.refreshStartupResources(t.Context()))
	row, err = f.b.Services.Resources.Get(t.Context(), p, row.ID)
	need(t, err)
	if row.State != resource.Available {
		t.Fatal(row)
	}
}

func TestResourceDisconnectPreservesAuthorizationHistory(t *testing.T) {
	t.Parallel()
	f := newResourceAccessFixture(t, func(cfg *config.Config, _ string) { cfg.Sandbox.Enabled = false })
	grant := f.grant(t, f.storage.ID, "files", []policy.Action{policy.Read}, policy.Scope{}, nil)
	observer, err := executionAuthorization(f.b.store, f.execution, func(ctx context.Context) error {
		_, err := f.ad.storageBinding(ctx, f.execution.Principal, f.storage.ID, "/read", policy.Read)
		return err
	})
	need(t, err)
	defer observer.Watch.Close()
	_, err = observer.Check(t.Context())
	need(t, err)
	need(t, f.b.Services.Resources.Disconnect(t.Context(), f.owner, resource.DisconnectRequest{ID: f.storage.ID, Mutation: core.Mutation{IdempotencyKey: "disconnect", ExpectedVersion: f.storage.Version}}))
	select {
	case <-observer.Watch.Changes():
	default:
		t.Fatal("disconnect did not notify running authorization")
	}
	_, err = observer.Check(t.Context())
	if err == nil {
		t.Fatal("disconnected authorization remains valid")
	}
	rows, err := policy.NewRepository(f.b.store).Grants(t.Context(), f.owner.OwnerID, policy.Query{ResourceID: f.storage.ID})
	need(t, err)
	if len(rows.Items) != 1 || rows.Items[0].ID != grant.ID || rows.Items[0].RevokedAt != nil {
		t.Fatal("disconnect revoked authorization")
	}
}
