//go:build linux && (amd64 || arm64)

package app

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/filetools"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/platform/sandbox"
	"molr.ai/runtime/internal/policy"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "shell-forward-test-server" {
		shellForwardTestServer(os.Args[2])
		os.Exit(0)
	}
	if len(os.Args) == 2 && os.Args[1] == "sandbox-supervisor" {
		os.Exit(sandbox.SupervisorMain())
	}
	if len(os.Args) > 1 && os.Args[1] == "sandbox-helper" {
		os.Exit(sandbox.HelperMain(os.Args[2:]))
	}
	os.Exit(m.Run())
}
func TestSandboxIntegrationRealToolAndAudit(t *testing.T) {
	if os.Getenv("MOLR_SANDBOX_INTEGRATION") != "1" {
		t.Skip("run scripts/test-sandbox-linux.sh")
	}
	cfg := testConfig(t)
	cfg.Sandbox.Enabled = true
	cfg.Sandbox.RootFS = t.TempDir()
	cfg.Sandbox.CgroupRoot = os.Getenv("MOLR_SANDBOX_CGROUP")
	cfg.Sandbox.MinFreeDiskBytes = 1
	work, err := os.MkdirTemp("/dev/shm", "molr-app-sandbox-")
	need(t, err)
	defer os.RemoveAll(work)
	cfg.Sandbox.WorkRoot = work
	for _, p := range []string{"bin", "usr/bin", "work", "global", "tmp", "proc", "dev", "resources", "molr"} {
		need(t, os.MkdirAll(filepath.Join(cfg.Sandbox.RootFS, p), 0755))
	}
	busy, err := os.ReadFile("/usr/bin/busybox")
	need(t, err)
	need(t, os.WriteFile(filepath.Join(cfg.Sandbox.RootFS, "bin/busybox"), busy, 0755))
	need(t, os.Symlink("busybox", filepath.Join(cfg.Sandbox.RootFS, "bin/sh")))
	exe, err := os.Executable()
	need(t, err)
	need(t, os.Chmod(exe, 0755))
	b, err := Build(t.Context(), cfg)
	need(t, err)
	defer b.Close()
	p, _ := ownerSession(t, b)
	e := core.Execution{Principal: core.Principal{OwnerID: p.OwnerID, SubjectID: "shell-assistant", Kind: core.ActorAssistant, ExecutionID: "shell-run"}, AssistantID: "shell-assistant", OperationID: "shell-operation"}
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "business.assistants", persistence.Key(p.OwnerID, e.AssistantID), assistant.Assistant{ID: e.AssistantID, OwnerID: p.OwnerID, State: assistant.Active, Version: 1}); err != nil {
			return err
		}
		return tx.Put(ctx, "agent.jobs", persistence.Key(p.OwnerID, "shell-job"), scheduler.Job{State: scheduler.Running, ExecutionID: e.Principal.ExecutionID, Event: event.AssistantEvent{ID: "shell-job", OwnerID: p.OwnerID, AssistantID: e.AssistantID}})
	}))
	compute, err := b.Services.Resources.Connect(t.Context(), p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "compute"}, Kind: resource.Compute, Provider: "local-sandbox", Name: "Local sandbox"})
	need(t, err)
	fileTools := newResourceFileTools(resourceFileOptions{adapters: adapters{store: b.store, policy: b.sandbox.policy, dataDir: cfg.DataDir, storageConfig: cfg.Sandbox}})
	_, err = fileTools.Write(t.Context(), e, tools.FileWriteRequest{FileTarget: tools.FileTarget{Path: "/work/from-file"}, Text: "before-shell"})
	need(t, err)
	global := platform.UserGlobal(cfg.DataDir, p.OwnerID)
	need(t, os.MkdirAll(global, 0700))
	need(t, os.WriteFile(filepath.Join(global, "MEMORY.md"), []byte("shared-file"), 0600))
	g, err := b.Services.Grants.Grant(t.Context(), p, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "allow-shell"}, AssistantID: e.AssistantID, ResourceID: compute.ID, Actions: []policy.Action{policy.Execute}, Constraints: testResourceConstraints(resource.Compute)})
	need(t, err)
	executor := tools.NewExecutor(b.store, tools.Options{Authorizer: b.sandbox.policy, Operations: b.Services.Operations.(operation.Service), ShellAvailable: b.sandbox.available, ShellExecute: b.sandbox.execute})
	definitions, err := executor.Definitions(t.Context(), e.Principal)
	need(t, err)
	if len(definitions) != 1 {
		t.Fatalf("definitions: %+v", definitions)
	}
	directory := t.TempDir()
	need(t, os.Mkdir(filepath.Join(directory, "approved"), 0700))
	storage, err := b.Services.Resources.Connect(t.Context(), p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "storage"}, Kind: resource.Storage, Provider: "local-directory", Name: "files", Binding: resource.Binding{RootPath: directory}})
	need(t, err)
	_, err = b.Services.Grants.Grant(t.Context(), p, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "allow-files"}, AssistantID: e.AssistantID, ResourceID: storage.ID, Actions: []policy.Action{policy.Read, policy.Write}, Scope: policy.Scope{PathPrefixes: []string{"/approved"}}})
	need(t, err)
	catalog := resourceCatalog{adapters: adapters{store: b.store, policy: b.sandbox.policy, dataDir: cfg.DataDir, storageConfig: cfg.Sandbox}, shell: b.sandbox}
	listing, err := catalog.list(t.Context(), e.Principal)
	need(t, err)
	var available resourceListing
	need(t, json.Unmarshal(listing, &available))
	if !lookupOperation(t, available, storage.ID, "/approved", policy.Write, "shell_execute").Available || !available.BuiltInStorage[0].Available {
		t.Fatal("ready resources reported unavailable")
	}
	args, _ := json.Marshal(tools.ShellRequest{ComputeID: compute.ID, Script: "read shared < /global/MEMORY.md; test \"$shared\" = shared-file || exit 7; if echo forbidden > /global/cannot-write; then exit 8; fi; echo shell-memory > /work/MEMORY.md; read value < /work/from-file; test \"$value\" = before-shell || exit 3; echo shell-ok; echo changed > /work/result; echo external > " + resourceShellPath(storage.ID, "/approved") + "/result", Files: []tools.ShellFile{{ResourceID: storage.ID, Path: "/approved", Write: true}}})
	call := tools.Call{Execution: e, Name: "shell_execute", Arguments: args}
	result, err := executor.Execute(t.Context(), call)
	need(t, err)
	var output struct {
		Exit   struct{ Code int }
		Stdout string
	}
	need(t, json.Unmarshal(result.Content, &output))
	if output.Exit.Code != 0 || output.Stdout != "shell-ok\n" {
		t.Fatalf("tool result: %s", result.Content)
	}
	content, err := (newResourceFileTools(resourceFileOptions{adapters: catalog.adapters})).Read(t.Context(), e, tools.FileReadRequest{FileTarget: tools.FileTarget{ResourceID: storage.ID, Path: "/approved/result"}})
	need(t, err)
	var file struct{ Text string }
	need(t, json.Unmarshal(content, &file))
	if file.Text != "external\n" {
		t.Fatal("file tool and Shell refer to different storage")
	}
	memories, err := b.Services.Memories.List(t.Context(), e.Principal, memory.Query{Scope: memory.Assistant})
	need(t, err)
	if len(memories.Items) != 1 || memories.Items[0].Text != "shell-memory\n" {
		t.Fatal("shell memory absent from memory service", memories)
	}

	_, err = executor.Execute(t.Context(), call)
	need(t, err)
	records, err := b.Services.Audit.List(t.Context(), p, audit.Query{Action: "sandbox.finish"})
	need(t, err)
	if len(records.Items) != 1 {
		t.Fatalf("audit: %+v", records)
	}
	need(t, b.Services.Grants.Revoke(t.Context(), p, policy.RevokeRequest{Mutation: core.Mutation{IdempotencyKey: "revoke-shell", ExpectedVersion: g.Version}, GrantID: g.ID}))
	workFile, err := fileTools.Read(t.Context(), e, tools.FileReadRequest{FileTarget: tools.FileTarget{Path: "/work/result"}})
	need(t, err)
	var workspaceRead filetools.ReadResult
	need(t, json.Unmarshal(workFile, &workspaceRead))
	if workspaceRead.Text != "changed\n" {
		t.Fatal("file tools and Shell use different assistant workspaces")
	}
	definitions, err = executor.Definitions(t.Context(), e.Principal)
	need(t, err)
	if len(definitions) != 0 {
		t.Fatal("shell tool still advertised after revoke")
	}
	call.Execution.OperationID = "denied-shell"
	if _, err = executor.Execute(t.Context(), call); err == nil {
		t.Fatal("shell executed after revocation")
	}
}
