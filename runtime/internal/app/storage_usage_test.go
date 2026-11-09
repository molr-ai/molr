package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/accountview"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/installrecord"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
)

func TestStorageUsageScopesDatabaseAttachmentsAndWorkspaces(t *testing.T) {
	var cfg config.Config
	f := newMultiUserFixture(t, func(c *config.Config) { c.Sandbox.WorkRoot = t.TempDir(); cfg = *c })
	ctx := t.Context()
	for _, owner := range []core.ID{f.alice.OwnerID, f.bob.OwnerID} {
		root := platform.AssistantWorkspace(cfg.DataDir, cfg.Sandbox.WorkRoot, owner, "stored-assistant")
		need(t, os.MkdirAll(filepath.Join(root, "nested"), 0700))
		need(t, os.WriteFile(filepath.Join(root, "nested", "private.txt"), []byte("workspace"), 0600))
		global := platform.UserGlobal(cfg.DataDir, owner)
		need(t, os.MkdirAll(global, 0700))
		need(t, os.WriteFile(filepath.Join(global, "instructions.md"), []byte("shared"), 0600))
		need(t, f.b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			if err := tx.Put(ctx, "business.assistants", persistence.Key(owner, "stored-assistant"), map[string]any{"OwnerID": owner, "ID": "stored-assistant", "State": "dismissed", "Name": "PRIVATE-assistant"}); err != nil {
				return err
			}
			return tx.Put(ctx, "storage.files", persistence.Key(owner, "attachment"), map[string]any{"ID": string(owner) + "-attachment", "OwnerID": owner, "State": "ready", "Size": 25, "Name": "PRIVATE-attachment"})
		}))
	}
	identity := f.b.Services.Auth.(*auth.Manager)
	personal, err := storageUsage(ctx, f.b.store, identity, cfg, f.alice, false)
	need(t, err)
	if !personal.Database.Available || personal.Database.Bytes <= 0 || personal.DatabasePhysical {
		t.Fatal(personal)
	}
	if personal.Shared.Bytes != 6 || personal.Shared.Files != 1 || !personal.Shared.Available || personal.System != nil {
		t.Fatal(personal)
	}
	if personal.Workspaces.Bytes != 9 || personal.Workspaces.Files != 1 || !personal.Workspaces.Available {
		t.Fatal(personal.Workspaces)
	}
	if personal.Attachments.Bytes != 25 || personal.Attachments.Files != 1 || !personal.Attachments.Available {
		t.Fatal(personal.Attachments)
	}
	global, err := storageUsage(ctx, f.b.store, identity, cfg, f.admin, true)
	need(t, err)
	if !global.Database.Available || !global.DatabasePhysical || global.Database.Bytes <= personal.Database.Bytes {
		t.Fatal(global.Database, personal.Database)
	}
	if global.Shared.Bytes != 12 || global.Shared.Files != 2 || !global.Shared.Available || global.System == nil || !global.System.Available || global.System.Bytes == 0 {
		t.Fatal(global)
	}
	if global.Workspaces.Bytes != 18 || global.Workspaces.Files != 2 || !global.Workspaces.Available || global.Attachments.Bytes != 50 {
		t.Fatal(global)
	}
	if _, err := storageUsage(ctx, f.b.store, identity, cfg, f.alice, true); core.CodeOf(err) != core.Forbidden {
		t.Fatal(err)
	}
	rawPersonal, err := json.Marshal(personal)
	need(t, err)
	if strings.Contains(string(rawPersonal), "System") {
		t.Fatal("system storage exposed to account view")
	}
	raw, err := json.Marshal(global)
	need(t, err)
	if strings.Contains(string(raw), "PRIVATE") || strings.Contains(string(raw), cfg.DataDir) || strings.Contains(string(raw), "stored-assistant") {
		t.Fatal("private storage details exposed")
	}
}

func TestDirectoryStorageIgnoresLinksAndReportsUnavailable(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	need(t, os.WriteFile(filepath.Join(root, "normal"), []byte("123"), 0600))
	need(t, os.WriteFile(filepath.Join(outside, "secret"), []byte("private"), 0600))
	need(t, os.Symlink(outside, filepath.Join(root, "linked-directory")))
	need(t, os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "linked-file")))
	got := directoryStorage(t.Context(), root)
	if !got.Available || got.Bytes != 3 || got.Files != 1 {
		t.Fatal(got)
	}
	if got := directoryStorage(t.Context(), filepath.Join(root, "missing")); !got.Available || got.Bytes != 0 {
		t.Fatal(got)
	}
	if got := directoryStorage(t.Context(), filepath.Join(root, "linked-directory")); got.Available {
		t.Fatal("linked root accepted", got)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got := directoryStorage(ctx, root); got.Available {
		t.Fatal("canceled scan became zero", got)
	}
}

func TestDatabaseFileUsageIncludesJournalsWithoutFollowingLinks(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.db")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		need(t, os.WriteFile(path+suffix, []byte("123"), 0600))
	}
	if got := databaseFileUsage(path); !got.Available || got.Bytes != 9 || got.Files != 3 {
		t.Fatal(got)
	}
	need(t, os.Remove(path+"-wal"))
	if got := databaseFileUsage(path); !got.Available || got.Bytes != 6 {
		t.Fatal(got)
	}
	need(t, os.Symlink(path, path+"-wal"))
	if got := databaseFileUsage(path); got.Available {
		t.Fatal(got)
	}
}

func TestSystemStoragePartitionsRootsWithoutDoubleCounting(t *testing.T) {
	root := t.TempDir()
	cfg := config.Defaults(filepath.Join(root, "data"))
	database := filepath.Join(cfg.DataDir, "runtime.db")
	put := func(path, text string) {
		t.Helper()
		need(t, os.MkdirAll(filepath.Dir(path), 0700))
		need(t, os.WriteFile(path, []byte(text), 0600))
	}
	put(database, "database")
	put(database+"-wal", "journal")
	put(database+"-shm", "shared memory")
	workspace := platform.AssistantWorkspace(cfg.DataDir, "", "alice", "assistant")
	put(filepath.Join(workspace, "file"), "workspace")
	shared := platform.UserGlobal(cfg.DataDir, "alice")
	put(filepath.Join(shared, "memory.md"), "shared")
	ready := filepath.Join(cfg.DataDir, "files", "objects", "ready")
	put(ready, "ready attachment")
	// All of these are service files, including uploads without a ready record.
	put(filepath.Join(cfg.DataDir, "files", "staging", "upload"), "12")
	put(filepath.Join(cfg.DataDir, "files", "objects", "orphan"), "123")
	put(filepath.Join(cfg.DataDir, "logs", "service.log"), "1234")
	put(filepath.Join(cfg.DataDir, "users", "metadata"), "12345")
	cache := filepath.Join(root, "cache")
	put(filepath.Join(cache, "components", "browser"), "123456")
	configFile := filepath.Join(root, "config", "server.json")
	put(configFile, "1234567")
	paths := []storagePath{{cfg.DataDir, true}, {filepath.Join(cache, "components"), true}, {cache, true}, {configFile, false}, {filepath.Dir(configFile), true}, {cfg.DataDir, true}, {workspace, true}, {shared, true}}
	got := systemStorage(t.Context(), cfg, database, map[string]bool{ready: true}, paths)
	if got != (accountview.StorageSize{Bytes: 27, Files: 6, Available: true}) {
		t.Fatal(got)
	}
	// Broader parents do not count business roots or already measured files twice.
	got = systemStorage(t.Context(), cfg, database, map[string]bool{ready: true}, append(paths, storagePath{root, true}))
	if got != (accountview.StorageSize{Bytes: 27, Files: 6, Available: true}) {
		t.Fatal(got)
	}
	need(t, os.Symlink(cache, filepath.Join(root, "cache-link")))
	got = systemStorage(t.Context(), cfg, database, nil, []storagePath{{filepath.Join(root, "cache-link"), true}})
	if got.Available {
		t.Fatal("linked root accepted", got)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if got := systemStorage(canceled, cfg, database, nil, paths); got.Available {
		t.Fatal(got)
	}
}

func TestStorageSystemPathsRequireMatchingInstallation(t *testing.T) {
	root := t.TempDir()
	programRoot := filepath.Join(root, "programs")
	program := filepath.Join(programRoot, "default")
	configRoot := filepath.Join(root, "config", "molr", "default")
	data := filepath.Join(root, "data", "default")
	cache := filepath.Join(root, "cache", "default")
	record := installrecord.Record{Schema: 1, State: "installed", Mode: "server", Name: "molr", User: "fixture", UID: 1000, GID: 1000, OwnerUID: 1000, ProgramRoot: programRoot, DataDir: data, ManagedData: true, Updates: installrecord.Updates{Channel: "stable"}, Paths: installrecord.Paths{Program: program, Config: configRoot, Data: data, Cache: cache, Unit: filepath.Join(root, "config", "systemd", "user", "molr.service")}}
	need(t, record.Validate())
	need(t, os.MkdirAll(program, 0700))
	raw, err := json.Marshal(record)
	need(t, err)
	need(t, os.WriteFile(record.File(), raw, 0600))
	cfg := config.Defaults(data)
	cfg.ConfigPath = filepath.Join(configRoot, "server.json")
	executable := filepath.Join(program, "bin", "molr")
	has := func(paths []storagePath, path string) bool {
		for _, p := range paths {
			if p.path == path {
				return true
			}
		}
		return false
	}
	paths := storageSystemPaths(cfg, executable)
	if !has(paths, cache) || !has(paths, program) || !has(paths, configRoot) || !has(paths, filepath.Join(filepath.Dir(executable), "libexec", "browser")) {
		t.Fatal(paths)
	}
	cfg.ConfigPath = filepath.Join(root, "other.json")
	if paths := storageSystemPaths(cfg, executable); has(paths, cache) || has(paths, program) || has(paths, configRoot) {
		t.Fatal("unrelated installation included", paths)
	}
	// A development/test executable must not imply scanning its surrounding tree.
	paths = storageSystemPaths(cfg, filepath.Join(root, "test", "app.test"))
	if has(paths, root) || has(paths, filepath.Join(root, "libexec", "browser")) {
		t.Fatal(paths)
	}
}
