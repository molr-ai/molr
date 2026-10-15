package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image/color"
	"molr.ai/runtime/internal/agent/model"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	sm "molr.ai/runtime/internal/sandboxmanager"
)

func TestRemoteSandboxStorageRoutingAuthorizationAndContext(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("requires supported remote storage")
	}
	f := newResourceAccessFixture(t, func(cfg *config.Config, _ string) { cfg.Sandbox.Enabled = false })
	remoteCfg := testConfig(t)
	remoteRoot := t.TempDir()
	remoteCfg.Sandbox.Directories = []config.SandboxDirectory{{Path: remoteRoot}}
	backend, err := newSandboxBackend(t.Context(), remoteCfg, quotaTestStore(t))
	need(t, err)
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sandbox.db"))
	need(t, err)
	t.Cleanup(func() { store.Close() })
	manager, err := sm.New(t.Context(), store, backend)
	need(t, err)
	t.Cleanup(func() { manager.Close() })
	token := strings.Repeat("test-token-", 8)
	handler, err := sm.Handler(manager, core.NewSecret(token))
	need(t, err)
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	sum := sha256.Sum256(server.Certificate().Raw)
	f.ad.networkConfig.AllowedPrivateIPs = []string{"127.0.0.1"}
	router, err := newSandboxRouter(f.ad, nil, make([]byte, 32))
	need(t, err)
	files := &resourceFileTools{adapters: f.ad, router: router}
	router.files = files
	admin := func(action string, input any) (any, error) {
		raw, _ := json.Marshal(input)
		return router.admin(t.Context(), f.owner, action, raw)
	}
	result, err := admin("connect", map[string]any{"Name": "Remote Linux", "Endpoint": server.URL, "Fingerprint": hex.EncodeToString(sum[:]), "Token": token, "Mutation": core.Mutation{IdempotencyKey: "connect"}})
	need(t, err)
	connection := result.(sandboxView)
	persisted, err := persistence.Load[sandboxConnection](t.Context(), f.b.store, "sandbox.connections", persistence.Key(f.owner.OwnerID, connection.ID))
	need(t, err)
	encoded, _ := json.Marshal(persisted)
	if strings.Contains(string(encoded), token) {
		t.Fatal("service credential persisted in plaintext")
	}
	e := f.execution
	// Omitting a node never selects a connected remote computer.
	_, err = files.write(t.Context(), e, tools.FileWriteRequest{FileTarget: tools.FileTarget{Path: "note.txt"}, Text: "local"})
	need(t, err)
	localFile := filepath.Join(platform.AssistantWorkspace(f.ad.dataDir, f.ad.storageConfig.WorkRoot, e.Principal.OwnerID, e.AssistantID), "note.txt")
	content, err := os.ReadFile(localFile)
	need(t, err)
	if string(content) != "local" {
		t.Fatal("default file operation did not use local workspace")
	}
	_, err = files.write(t.Context(), e, tools.FileWriteRequest{FileTarget: tools.FileTarget{SandboxID: connection.ID, Path: "/work/note.txt"}, Text: "remote"})
	if core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("remote workspace was accepted", err)
	}
	remoteWork := platform.AssistantWorkspace(remoteCfg.DataDir, remoteCfg.Sandbox.WorkRoot, f.owner.OwnerID, e.AssistantID)
	if _, err := os.Stat(remoteWork); !os.IsNotExist(err) {
		t.Fatal("remote assistant workspace created", err)
	}
	result, err = admin("resource", map[string]any{"SandboxID": connection.ID, "Kind": "storage", "SourceID": directoryID(remoteRoot), "Name": "Remote files", "Mutation": core.Mutation{IdempotencyKey: "resource"}})
	need(t, err)
	storage := result.(resource.Resource)
	need(t, os.Mkdir(filepath.Join(remoteRoot, "allowed"), 0700))
	need(t, os.Mkdir(filepath.Join(remoteRoot, "secret"), 0700))
	need(t, os.WriteFile(filepath.Join(remoteRoot, "allowed", "file"), []byte("allowed"), 0600))
	need(t, os.WriteFile(filepath.Join(remoteRoot, "secret", "file"), []byte("secret"), 0600))
	target := tools.FileTarget{SandboxID: connection.ID, ResourceID: storage.ID, Path: "/allowed/file"}
	e.OperationID = "denied"
	_, err = files.read(t.Context(), e, tools.FileReadRequest{FileTarget: target})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("ungranted resource read", err)
	}
	grant := f.grant(t, storage.ID, "remote-read-grant", []policy.Action{policy.Read}, policy.Scope{PathPrefixes: []string{"/allowed"}}, nil)
	e.OperationID = "allowed"
	e.OperationID = "remote-search"
	searchRaw, err := files.search(t.Context(), e, tools.FileSearchRequest{FileTarget: tools.FileTarget{SandboxID: connection.ID, ResourceID: storage.ID, Path: "/allowed"}, Pattern: "allowed", Glob: []string{"**/fi?e"}})
	need(t, err)
	var searched struct {
		Matches  []struct{ Path, Text string }
		Complete bool
	}
	need(t, json.Unmarshal(searchRaw, &searched))
	if !searched.Complete || len(searched.Matches) != 1 || searched.Matches[0].Path != "/allowed/file" {
		t.Fatal("remote file search", string(searchRaw))
	}
	e.OperationID = "remote-read"
	out, err := files.read(t.Context(), e, tools.FileReadRequest{FileTarget: target})
	need(t, err)
	if !strings.Contains(string(out), "allowed") {
		t.Fatal(string(out))
	}

	// A remote resource never changes the target implicitly.
	_, err = files.read(t.Context(), e, tools.FileReadRequest{FileTarget: tools.FileTarget{ResourceID: storage.ID, Path: "/allowed/file"}})
	if core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("implicit remote routing accepted", err)
	}
	// Common move/delete apply both remote path scopes and return usable versions.
	moveWriteGrant := f.grant(t, storage.ID, "remote-write", []policy.Action{policy.Write}, policy.Scope{PathPrefixes: []string{"/allowed"}}, nil)
	source := tools.FileTarget{SandboxID: connection.ID, ResourceID: storage.ID, Path: "/allowed/moving"}
	need(t, os.WriteFile(filepath.Join(remoteRoot, "allowed/moving"), []byte("move me"), 0600))
	e.OperationID = "remote-moving-read"
	raw, err := files.read(t.Context(), e, tools.FileReadRequest{FileTarget: source})
	need(t, err)
	var original struct{ Version string }
	need(t, json.Unmarshal(raw, &original))
	e.OperationID = "remote-move-denied"
	_, err = files.move(t.Context(), e, tools.FileMoveRequest{FileTarget: source, ToPath: "/secret/moving", ExpectedVersion: original.Version})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("remote destination grant missing", err)
	}
	e.OperationID = "remote-move"
	raw, err = files.move(t.Context(), e, tools.FileMoveRequest{FileTarget: source, ToPath: "/allowed/nested/moved", ExpectedVersion: original.Version, CreateParents: true})
	need(t, err)
	var moved struct{ Version string }
	need(t, json.Unmarshal(raw, &moved))
	e.OperationID = "remote-delete"
	source.Path = "/allowed/nested/moved"
	_, err = files.delete(t.Context(), e, tools.FileDeleteRequest{FileTarget: source, ExpectedVersion: moved.Version})
	need(t, err)
	if _, err = os.Stat(filepath.Join(remoteRoot, "allowed/nested/moved")); !os.IsNotExist(err) {
		t.Fatal("remote deletion failed", err)
	}

	need(t, f.b.Services.Grants.Revoke(t.Context(), f.owner, policy.RevokeRequest{Mutation: core.Mutation{IdempotencyKey: "revoke-move-write", ExpectedVersion: moveWriteGrant.Version}, GrantID: moveWriteGrant.ID}))

	// Media snapshots retain their remote directory provenance.
	provider := mediaTestModel(t, f)
	png := testPNG(t, color.White)
	need(t, os.WriteFile(filepath.Join(remoteRoot, "allowed", "image.png"), png, 0600))
	e.OperationID = "remote-media"
	mediaRaw, err := files.read(t.Context(), e, tools.FileReadRequest{ModelID: provider.Model.ID, FileTarget: tools.FileTarget{SandboxID: connection.ID, ResourceID: storage.ID, Path: "/allowed/image.png"}})
	need(t, err)
	var mediaRead mediaReadResult
	need(t, json.Unmarshal(mediaRaw, &mediaRead))
	if len(mediaRead.Media) != 1 || mediaRead.Media[0].SandboxID != connection.ID {
		t.Fatal("media provenance lost")
	}
	prepared, err := files.prepareMedia(t.Context(), model.Request{Execution: e, Inputs: []model.Input{{Role: model.Tool, Media: mediaRead.Media}}}, provider)
	need(t, err)
	if !bytes.Equal(prepared[0].Media[0].Data, png) {
		t.Fatal("remote binary snapshot changed")
	}
	// Enable the local file environment: copies always land in the local workspace.
	files.adapters.storageConfig.Enabled = true
	router.adapters.storageConfig.Enabled = true
	e.OperationID = "download-binary"
	_, err = files.transfer(t.Context(), e, tools.FileTransferRequest{Source: tools.FileTarget{SandboxID: connection.ID, ResourceID: storage.ID, Path: "/allowed/image.png"}, Destination: tools.FileTarget{Path: "/work/copied.png"}})
	need(t, err)
	localRoot := platform.AssistantWorkspace(f.ad.dataDir, f.ad.storageConfig.WorkRoot, e.Principal.OwnerID, e.AssistantID)
	copied, err := os.ReadFile(filepath.Join(localRoot, "copied.png"))
	need(t, err)
	if !bytes.Equal(copied, png) {
		t.Fatal("binary transfer changed bytes")
	}
	e.OperationID = "download-no-overwrite"
	_, err = files.transfer(t.Context(), e, tools.FileTransferRequest{Source: tools.FileTarget{SandboxID: connection.ID, ResourceID: storage.ID, Path: "/allowed/image.png"}, Destination: tools.FileTarget{Path: "/work/copied.png"}})
	if core.CodeOf(err) != core.Conflict {
		t.Fatal("transfer overwrote an existing file", err)
	}
	writeGrant := f.grant(t, storage.ID, "remote-write-grant", []policy.Action{policy.Write}, policy.Scope{PathPrefixes: []string{"/allowed"}}, nil)
	e.OperationID = "upload-binary"
	_, err = files.transfer(t.Context(), e, tools.FileTransferRequest{Source: tools.FileTarget{Path: "/work/copied.png"}, Destination: tools.FileTarget{SandboxID: connection.ID, ResourceID: storage.ID, Path: "/allowed/uploaded.png"}})
	need(t, err)
	uploaded, err := os.ReadFile(filepath.Join(remoteRoot, "allowed", "uploaded.png"))
	need(t, err)
	if !bytes.Equal(uploaded, png) {
		t.Fatal("upload changed binary")
	}
	need(t, f.b.Services.Grants.Revoke(t.Context(), f.owner, policy.RevokeRequest{Mutation: core.Mutation{IdempotencyKey: "revoke-write", ExpectedVersion: writeGrant.Version}, GrantID: writeGrant.ID}))
	files.adapters.storageConfig.Enabled = false
	router.adapters.storageConfig.Enabled = false
	e.OperationID = "outside-scope"
	_, err = files.read(t.Context(), e, tools.FileReadRequest{FileTarget: tools.FileTarget{SandboxID: connection.ID, ResourceID: storage.ID, Path: "/secret/file"}})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("scope escaped", err)
	}
	need(t, os.Symlink(filepath.Join(remoteRoot, "secret", "file"), filepath.Join(remoteRoot, "allowed", "link")))
	e.OperationID = "symlink"
	_, err = files.read(t.Context(), e, tools.FileReadRequest{FileTarget: tools.FileTarget{SandboxID: connection.ID, ResourceID: storage.ID, Path: "/allowed/link"}})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("symlink escaped", err)
	}
	e.OperationID = "write-denied"
	_, err = files.write(t.Context(), e, tools.FileWriteRequest{FileTarget: target, Text: "bad", Overwrite: true})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("read grant permitted write", err)
	}
	need(t, f.b.Services.Grants.Revoke(t.Context(), f.owner, policy.RevokeRequest{Mutation: core.Mutation{IdempotencyKey: "revoke", ExpectedVersion: grant.Version}, GrantID: grant.ID}))
	e.OperationID = "after-revoke"
	_, err = files.read(t.Context(), e, tools.FileReadRequest{FileTarget: target})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("revoked grant remained usable", err)
	}
	// Disabling local commands does not disable local or remote storage.
	e.OperationID = "local-disabled"
	_, err = files.read(t.Context(), e, tools.FileReadRequest{FileTarget: tools.FileTarget{SandboxID: "local", Path: "note.txt"}})
	need(t, err)
	_, err = admin("resource", map[string]any{"SandboxID": connection.ID, "Kind": "storage", "SourceID": "host-root", "Name": "Escape", "Mutation": core.Mutation{IdempotencyKey: "escape"}})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("undeployed directory accepted", err)
	}
}

func TestStandaloneSandboxRequiresTLS(t *testing.T) {
	cfg := testConfig(t)
	if err := ServeSandbox(t.Context(), cfg, core.NewSecret(strings.Repeat("a", 40))); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("standalone sandbox accepted a proxy/plaintext listener", err)
	}
}
