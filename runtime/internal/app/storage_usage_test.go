package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
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
		need(t, f.b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			if err := tx.Put(ctx, "business.assistants", persistence.Key(owner, "stored-assistant"), map[string]any{"OwnerID": owner, "ID": "stored-assistant", "State": "dismissed", "Name": "PRIVATE-assistant"}); err != nil {
				return err
			}
			return tx.Put(ctx, "storage.files", persistence.Key(owner, "attachment"), map[string]any{"OwnerID": owner, "State": "ready", "Size": 25, "Name": "PRIVATE-attachment"})
		}))
	}
	identity := f.b.Services.Auth.(*auth.Manager)
	personal, err := storageUsage(ctx, f.b.store, identity, cfg, f.alice, false)
	need(t, err)
	if !personal.Database.Available || personal.Database.Bytes <= 0 || personal.DatabasePhysical {
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
	if global.Workspaces.Bytes != 18 || global.Workspaces.Files != 2 || !global.Workspaces.Available || global.Attachments.Bytes != 50 {
		t.Fatal(global)
	}
	if _, err := storageUsage(ctx, f.b.store, identity, cfg, f.alice, true); core.CodeOf(err) != core.Forbidden {
		t.Fatal(err)
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
