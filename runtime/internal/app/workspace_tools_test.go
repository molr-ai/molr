package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
)

func TestWorkspaceFileToolsNeedNoStorageOrComputeGrant(t *testing.T) {
	for _, override := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "override"}[override], func(t *testing.T) {
			f := newResourceAccessFixture(t, func(cfg *config.Config, _ string) {
				cfg.Sandbox.Enabled = false
				if override {
					cfg.Sandbox.WorkRoot = t.TempDir()
				}
			})
			fs := &resourceFileTools{adapters: f.ad}
			ex := tools.NewExecutor(f.b.store, tools.Options{Authorizer: f.ad.policy, Operations: f.b.Services.Operations.(operation.Service), FileRead: fs.read, FileList: fs.list, FileWrite: fs.write, FileEdit: fs.edit})
			call := func(name string, input any, id core.ID) (json.RawMessage, error) {
				raw, _ := json.Marshal(input)
				e := f.execution
				e.OperationID = id
				result, err := ex.Execute(t.Context(), tools.Call{Execution: e, Name: name, Arguments: raw})
				return result.Content, err
			}
			_, err := call("file_list", map[string]any{"path": "."}, "list-empty")
			need(t, err)
			_, err = call("file_write", map[string]any{"path": "note.txt", "text": "hello", "overwrite": false}, "write-workspace")
			need(t, err)
			raw, err := call("file_read", map[string]any{"path": "/work/note.txt"}, "read-workspace")
			need(t, err)
			var read fileReadResult
			need(t, json.Unmarshal(raw, &read))
			if read.Text != "hello" || read.ResourceID != "" {
				t.Fatal("workspace read failed")
			}
			_, err = call("file_edit", map[string]any{"path": "note.txt", "old_text": "hello", "new_text": "edited", "expected_version": read.Version}, "edit-workspace")
			need(t, err)
			root := platform.AssistantWorkspace(f.ad.dataDir, f.ad.storageConfig.WorkRoot, f.owner.OwnerID, f.execution.AssistantID)
			data, err := os.ReadFile(filepath.Join(root, "note.txt"))
			need(t, err)
			if string(data) != "edited" {
				t.Fatal("wrong workspace path")
			}
			_, err = call("file_read", map[string]any{"resource_id": f.storage.ID, "path": "/read/data"}, "denied-external")
			if core.CodeOf(err) != core.Forbidden {
				t.Fatalf("workspace capability implied external grant: %v", err)
			}
			out := f.list(t)
			if !out.BuiltInStorage[0].Available || out.BuiltInStorage[1].Available {
				t.Fatal("workspace coupled to disabled Shell")
			}
			for _, op := range out.BuiltInStorage[0].Operations {
				if op.Via == "shell_execute" && op.Available {
					t.Fatal("workspace implied compute permission")
				}
			}
			if override {
				_, err = f.b.Services.Resources.Connect(t.Context(), f.owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "expose-workspaces"}, Kind: resource.Storage, Provider: "local-directory", Name: "invalid", Binding: resource.Binding{RootPath: f.ad.storageConfig.WorkRoot}})
				if core.CodeOf(err) != core.Forbidden {
					t.Fatal("managed workspace root exposed as external resource")
				}
			}
		})
	}
}

func TestWorkspaceIdentityAndSymlinkIsolation(t *testing.T) {
	f := newResourceAccessFixture(t, func(cfg *config.Config, _ string) { cfg.Sandbox.Enabled = false })
	fs := &resourceFileTools{adapters: f.ad}
	other := f.execution
	other.AssistantID = "other"
	other.Principal.SubjectID = "other"
	other.Principal.ExecutionID = "other-run"
	need(t, f.b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "business.assistants", persistence.Key(f.owner.OwnerID, other.AssistantID), assistant.Assistant{ID: other.AssistantID, OwnerID: f.owner.OwnerID, State: assistant.Active, Version: 1}); err != nil {
			return err
		}
		return tx.Put(ctx, "agent.jobs", persistence.Key(f.owner.OwnerID, "other-job"), scheduler.Job{State: scheduler.Running, ExecutionID: other.Principal.ExecutionID, Event: event.AssistantEvent{ID: "other-job", OwnerID: f.owner.OwnerID, AssistantID: other.AssistantID}})
	}))
	_, err := fs.write(t.Context(), f.execution, tools.FileWriteRequest{FileTarget: tools.FileTarget{Path: "/work/private.txt"}, Text: "private"})
	need(t, err)
	_, err = fs.read(t.Context(), other, tools.FileReadRequest{FileTarget: tools.FileTarget{Path: "/work/private.txt"}})
	if core.CodeOf(err) != core.NotFound {
		t.Fatal("another assistant read workspace data")
	}
	otherRoot := platform.AssistantWorkspace(f.ad.dataDir, "", f.owner.OwnerID, other.AssistantID)
	root := platform.AssistantWorkspace(f.ad.dataDir, "", f.owner.OwnerID, f.execution.AssistantID)
	need(t, os.Symlink(root, filepath.Join(otherRoot, "link")))
	if _, err = fs.read(t.Context(), other, tools.FileReadRequest{FileTarget: tools.FileTarget{Path: "/work/link/private.txt"}}); core.CodeOf(err) != core.Forbidden {
		t.Fatal("workspace symlink escaped identity")
	}
	forged := other
	forged.AssistantID = f.execution.AssistantID
	if _, err = fs.list(t.Context(), forged, tools.FileListRequest{FileTarget: tools.FileTarget{Path: "/work"}}); err == nil {
		t.Fatal("forged assistant identity accepted")
	}
}

func TestGlobalFilesReadSameOwnerMemoryAndRejectWrites(t *testing.T) {
	f := newResourceAccessFixture(t, func(cfg *config.Config, _ string) { cfg.Sandbox.Enabled = false })
	fs := &resourceFileTools{adapters: f.ad}
	root := platform.UserGlobal(f.ad.dataDir, f.owner.OwnerID)
	need(t, os.MkdirAll(root, 0700))
	need(t, os.WriteFile(filepath.Join(root, "MEMORY.md"), []byte("Shared owner preference"), 0600))
	raw, err := fs.read(t.Context(), f.execution, tools.FileReadRequest{FileTarget: tools.FileTarget{Path: "/global/MEMORY.md"}, Offset: 1, Limit: 20})
	need(t, err)
	var read fileReadResult
	need(t, json.Unmarshal(raw, &read))
	if read.Text != "Shared owner preference" {
		t.Fatal(read)
	}
	_, err = fs.write(t.Context(), f.execution, tools.FileWriteRequest{FileTarget: tools.FileTarget{Path: "/global/MEMORY.md"}, Text: "overwrite", Overwrite: true, ExpectedVersion: read.Version})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("global write allowed", err)
	}
	_, err = fs.list(t.Context(), f.execution, tools.FileListRequest{FileTarget: tools.FileTarget{Path: "/global/../work"}})
	if core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("noncanonical global path allowed", err)
	}
	page, err := f.b.Services.Memories.List(t.Context(), f.execution.Principal, memory.Query{Scope: memory.Global})
	need(t, err)
	if len(page.Items) != 1 || page.Items[0].Text != read.Text || page.Items[0].Path != "MEMORY.md" {
		t.Fatal("tools and memory see different files", page)
	}
}
