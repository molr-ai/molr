package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
 "encoding/base64"
	"encoding/json"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/catalog"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	sm "molr.ai/runtime/internal/sandboxmanager"
)

func TestSandboxPresentationPreservesCredentialsAndOwnerBoundary(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("requires a supported remote server")
	}
	f := newResourceAccessFixture(t, func(cfg *config.Config, _ string) { cfg.Sandbox.Enabled = false })
	cfg := testConfig(t)
	root := t.TempDir()
	cfg.Sandbox.Directories = []config.SandboxDirectory{{Path: root}}
	backend, err := newSandboxBackend(t.Context(), cfg, quotaTestStore(t))
	need(t, err)
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sandbox.db"))
	need(t, err)
	t.Cleanup(func() { store.Close() })
	manager, err := sm.New(t.Context(), store, backend)
	need(t, err)
	t.Cleanup(func() { manager.Close() })
	token := strings.Repeat("private-test-token-", 4)
	handler, err := sm.Handler(manager, core.NewSecret(token))
	need(t, err)
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	sum := sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)
	pin := hex.EncodeToString(sum[:])
	f.ad.networkConfig.AllowedPrivateIPs = []string{"127.0.0.1"}
	router, err := newSandboxRouter(f.ad, nil, make([]byte, 32))
	need(t, err)
	call := func(p core.Principal, action string, input any) (any, error) {
		raw, _ := json.Marshal(input)
		return router.admin(t.Context(), p, action, raw)
	}
	probe := func(endpoint, fingerprint, credential string) sandboxDiagnostic {
		t.Helper()
		out, err := call(f.owner, "test", map[string]any{"Endpoint": endpoint, "ConnectionCode": sandboxConnectionCode(fingerprint, credential)})
		need(t, err)
		d := out.(sandboxDiagnostic)
		raw, _ := json.Marshal(d)
		if strings.Contains(string(raw), credential) || strings.Contains(string(raw), root) {
			t.Fatal("diagnostic leaked secrets or host paths")
		}
		return d
	}
	if d := probe(server.URL, pin, token); !d.Connected || d.Info == nil {
		t.Fatalf("valid probe: %+v", d)
	}
	for _, tc := range []struct{ endpoint, pin, token, reason, field string }{
		{"http://bad.invalid", pin, token, "sandbox_endpoint_invalid", "Endpoint"},
		{server.URL, "bad-pin", token, "sandbox_connection_code_invalid", "ConnectionCode"},
		{server.URL, strings.Repeat("0", 64), token, "sandbox_certificate_mismatch", "ConnectionCode"},
		{server.URL, pin, strings.Repeat("wrong-token", 4), "sandbox_auth_failed", "ConnectionCode"},
		{server.URL, pin, "short", "sandbox_connection_code_invalid", "ConnectionCode"},
	} {
		d := probe(tc.endpoint, tc.pin, tc.token)
		if d.Connected || string(d.Reason) != tc.reason || d.Field != tc.field {
			t.Fatalf("probe reason: %+v", d)
		}
	}
	connections, err := persistence.Scan[sandboxConnection](t.Context(), f.b.store, "sandbox.connections")
	need(t, err)
	if len(connections) != 0 {
		t.Fatal("testing persisted a connection")
	}
	out, err := call(f.owner, "connect", map[string]any{"Name": "Original", "Endpoint": server.URL, "ConnectionCode": sandboxConnectionCode(pin, token), "Mutation": core.Mutation{IdempotencyKey: "connect"}})
	need(t, err)
	view := out.(sandboxView)
	load := func() sandboxConnection {
		c, e := persistence.Load[sandboxConnection](t.Context(), f.b.store, "sandbox.connections", persistence.Key(catalog.Scope, view.ID))
		need(t, e)
		return c
	}
	before := load()
	epoch, err := router.epoch(t.Context(), f.owner.OwnerID)
	need(t, err)
	out, err = call(f.owner, "test", map[string]any{"ID": view.ID})
	need(t, err)
	if !out.(sandboxDiagnostic).Connected {
		t.Fatal("saved probe failed")
	}
	_, err = call(f.owner, "test", map[string]any{"ID": view.ID, "Endpoint": "https://other.invalid"})
	if core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("saved credential redirected", err)
	}
	rename := map[string]any{"ID": view.ID, "Name": "New name", "Mutation": core.Mutation{IdempotencyKey: "rename", ExpectedVersion: view.Version}}
	out, err = call(f.owner, "rename", rename)
	need(t, err)
	if out.(sandboxView).Name != "New name" || out.(sandboxView).Version != view.Version+1 {
		t.Fatal("rename not applied")
	}
	_, err = call(f.owner, "rename", rename)
	need(t, err)
	after := load()
	nowEpoch, err := router.epoch(t.Context(), f.owner.OwnerID)
	need(t, err)
	if !bytes.Equal(before.Secret, after.Secret) || before.Digest != after.Digest || before.View.Endpoint != after.View.Endpoint || before.View.PublicKeySHA256 != after.View.PublicKeySHA256 || epoch != nowEpoch {
		t.Fatal("rename changed connection authority")
	}
	rename["Name"] = "Stale overwrite"
	_, err = call(f.owner, "rename", rename)
	if core.CodeOf(err) != core.Conflict {
		t.Fatal("stale rename accepted", err)
	}
	out, err = call(f.owner, "resource", map[string]any{"SandboxID": view.ID, "Kind": "storage", "SourceID": directoryID(root), "Name": "Files", "Mutation": core.Mutation{IdempotencyKey: "files"}})
	need(t, err)
	remote := out.(resource.Resource)
	out, err = call(f.owner, "details", map[string]any{"ID": view.ID})
	need(t, err)
	details := out.([]sandboxResourceDetail)
	if len(details) != 1 || details[0].Capacity == nil || details[0].Capacity.StorageBytes <= 0 {
		t.Fatal("remote filesystem capacity missing", details)
	}
	if len(details) != 1 || details[0].ID != remote.ID || details[0].SourceID != directoryID(root) || details[0].Path != "" {
		t.Fatal("remote details exposed incorrect metadata", details)
	}
	out, err = call(f.owner, "details", map[string]any{"ID": "local"})
	need(t, err)
	details = out.([]sandboxResourceDetail)
	if len(details) != 1 || details[0].Capacity == nil || details[0].Capacity.StorageBytes <= 0 {
		t.Fatal("local filesystem capacity missing", details)
	}
	if len(details) != 1 || details[0].ID != f.storage.ID || details[0].Path != f.root || details[0].SourceID != directoryID(f.root) {
		t.Fatal("local source missing", details)
	}
	for _, action := range []string{"test", "details", "rename"} {
		_, err = call(f.execution.Principal, action, map[string]any{"ID": view.ID})
		if core.CodeOf(err) != core.Forbidden {
			t.Fatal("assistant reached owner sandbox API", action, err)
		}
	}
	need(t, f.b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "auth.bootstrap", "owner", struct{ AccountID core.ID }{f.owner.OwnerID}); err != nil {
			return err
		}
		if err := tx.Put(ctx, "auth.accounts", "other-owner", struct{ Account auth.Account }{auth.Account{ID: "other-owner", GroupID: "members"}}); err != nil {
			return err
		}
		return tx.Put(ctx, "auth.groups", "members", auth.UserGroup{ID: "members"})
	}))
	other := f.owner
	other.OwnerID = "other-owner"
	other.SubjectID = "other-owner"
	for _, action := range []string{"test", "details", "rename"} {
		input := map[string]any{"ID": view.ID, "Name": "Cross owner", "Mutation": core.Mutation{IdempotencyKey: "other", ExpectedVersion: after.View.Version}}
		_, err = call(other, action, input)
		if err == nil {
			t.Fatal("cross-owner access", action)
		}
	}
}

// Deliberately permits malformed fields so diagnostics can test hostile input.
func sandboxConnectionCode(publicKey, token string) string {
 data, _ := json.Marshal(map[string]string{"public_key_sha256":publicKey,"token":token})
 return "molr-sandbox-v1." + base64.RawURLEncoding.EncodeToString(data)
}
