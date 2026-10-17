package app

import (
	"context"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/policy"
	"os"
	"path/filepath"
	"testing"

	"molr.ai/runtime/internal/config"
	sm "molr.ai/runtime/internal/sandboxmanager"
)

func TestSandboxDirectorySelectionAndPolicyRecheck(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	need(t, err)
	private, err := filepath.EvalSymlinks(t.TempDir())
	need(t, err)
	child := filepath.Join(root, "project-a")
	need(t, os.Mkdir(child, 0700))
	other, err := filepath.EvalSymlinks(t.TempDir())
	need(t, err)
	ad := adapters{dataDir: private, storageConfig: config.Sandbox{RestrictDirectories: true, Directories: []config.SandboxDirectory{{Path: root, ReadOnly: true}}}}
	q := sm.DirectoryRequest{SourceID: directoryID(root), Path: "project-a"}
	d, err := resolveSandboxDirectory(t.Context(), ad, q)
	need(t, err)
	if d.Path != child || !d.ReadOnly || d.ID == q.SourceID {
		t.Fatalf("incorrect child binding: %+v", d)
	}
	ref, err := directoryReference(d.ID)
	need(t, err)
	resolved, err := resolveSandboxDirectory(t.Context(), ad, ref)
	need(t, err)
	if resolved != d {
		t.Fatal("saved reference changed")
	}
	for _, bad := range []sm.DirectoryRequest{{SourceID: q.SourceID, Path: "../"}, {SourceID: q.SourceID, Path: other}, {Path: other}, {Path: private}} {
		if _, err := resolveSandboxDirectory(t.Context(), ad, bad); err == nil {
			t.Fatalf("accepted outside scope: %+v", bad)
		}
	}
	link := filepath.Join(root, "alias")
	need(t, os.Symlink(other, link))
	if _, err := resolveSandboxDirectory(t.Context(), ad, sm.DirectoryRequest{SourceID: q.SourceID, Path: "alias"}); err == nil {
		t.Fatal("accepted symlink")
	}
	ad.storageConfig.Directories = nil
	if _, err := resolveSandboxDirectory(t.Context(), ad, ref); err == nil {
		t.Fatal("removed deployment root remained usable")
	}
	ad.storageConfig.RestrictDirectories = false
	d, err = resolveSandboxDirectory(t.Context(), ad, sm.DirectoryRequest{Path: other})
	need(t, err)
	if d.Path != other || d.ReadOnly {
		t.Fatal("unrestricted path not connected")
	}
	ref, err = directoryReference(d.ID)
	need(t, err)
	ad.storageConfig.RestrictDirectories = true
	if _, err := resolveSandboxDirectory(t.Context(), ad, ref); err == nil {
		t.Fatal("narrowing deployment retained unrestricted reference")
	}
}

func TestRemoteDirectoryReferenceChecksScopeAfterDeploymentChanges(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	need(t, err)
	private, err := filepath.EvalSymlinks(t.TempDir())
	need(t, err)
	ad := adapters{dataDir: private, storageConfig: config.Sandbox{Enabled: true}}
	b := &remoteSandboxBackend{cfg: config.Config{DataDir: private, Sandbox: ad.storageConfig}, files: &resourceFileTools{adapters: ad}}
	d, err := b.Directory(t.Context(), sm.DirectoryRequest{Path: root})
	need(t, err)
	identity := sm.Identity{OwnerID: "owner", AssistantID: "assistant", ExecutionID: "execution", OperationID: "operation"}
	request := sm.Request{Identity: identity, Scopes: []sm.Scope{{ResourceID: "files", DirectoryID: d.ID, Path: "/project-a", Read: true, Write: true}}}
	ctx := context.WithValue(t.Context(), sandboxRequestKey{}, request)
	target := tools.FileTarget{ResourceID: "files", Path: "/project-a/report.txt"}
	actual, name, err := b.resolve(ctx, identity.Execution(), target, policy.Read)
	need(t, err)
	if actual != root || name != "project-a/report.txt" {
		t.Fatalf("wrong remote target: %s %s", actual, name)
	}
	if _, _, err = b.resolve(ctx, identity.Execution(), tools.FileTarget{ResourceID: "files", Path: "/other"}, policy.Read); err == nil {
		t.Fatal("expanded assistant scope")
	}
	b.cfg.Sandbox.RestrictDirectories = true
	b.files.adapters.storageConfig = b.cfg.Sandbox
	if _, _, err = b.resolve(ctx, identity.Execution(), target, policy.Read); err == nil {
		t.Fatal("retained old path after restricting deployment")
	}
	b.cfg.Sandbox.Directories = []config.SandboxDirectory{{Path: root, ReadOnly: true}}
	b.files.adapters.storageConfig = b.cfg.Sandbox
	if _, _, err = b.resolve(ctx, identity.Execution(), target, policy.Write); err == nil {
		t.Fatal("wrote read-only remote directory")
	}
}
