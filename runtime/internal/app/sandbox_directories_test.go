package app

import (
	"context"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/policy"
	"os"
	"path/filepath"
	"testing"

	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	sm "molr.ai/runtime/internal/sandboxmanager"
)

func TestSandboxDirectoryTrailingSeparatorsAndErrors(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	need(t, err)
	private, err := filepath.EvalSymlinks(t.TempDir())
	need(t, err)
	child := filepath.Join(root, "project")
	need(t, os.Mkdir(child, 0700))
	file := filepath.Join(root, "file.txt")
	need(t, os.WriteFile(file, []byte("fixture"), 0600))
	link := filepath.Join(root, "alias")
	need(t, os.Symlink(child, link))
	ad := adapters{dataDir: private, storageConfig: config.Sandbox{Directories: []config.SandboxDirectory{{Path: root}}}}
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	sep := string(filepath.Separator)
	for _, q := range []sm.DirectoryRequest{
		{Path: child + sep}, {Path: child + sep + sep},
		{SourceID: directoryID(root), Path: "project" + sep},
		{SourceID: directoryID(root), Path: "project" + sep + sep},
	} {
		d, err := resolveSandboxDirectory(t.Context(), ad, q)
		need(t, err)
		if d.Path != child {
			t.Fatalf("path = %q, want %q", d.Path, child)
		}
		ref, err := directoryReference(d.ID)
		need(t, err)
		if ref.Path != child && ref.Path != "project" {
			t.Fatalf("reference not normalized: %+v", ref)
		}
		again, err := resolveSandboxDirectory(t.Context(), ad, ref)
		need(t, err)
		if again.ID != d.ID || again.Path != d.Path {
			t.Fatalf("reference changed after normalization: %+v", again)
		}
	}
	for input, want := range map[string]string{"~/": root, "~///": root, "~/project///": child} {
		d, err := resolveSandboxDirectory(t.Context(), ad, sm.DirectoryRequest{Path: input})
		need(t, err)
		if d.Path != want {
			t.Fatalf("home alias %q resolved to %q, want %q", input, d.Path, want)
		}
		ref, err := directoryReference(d.ID)
		need(t, err)
		if ref.Path != want {
			t.Fatalf("home alias saved instead of absolute path: %+v", ref)
		}
	}
	for _, tc := range []struct {
		request sm.DirectoryRequest
		reason  core.Reason
	}{
		{sm.DirectoryRequest{Path: "project/"}, core.DirectoryAbsolutePathRequired},
		{sm.DirectoryRequest{Path: root + sep + "project" + sep + ".."}, core.DirectoryAbsolutePathRequired},
		{sm.DirectoryRequest{SourceID: directoryID(root), Path: "../"}, core.DirectoryRelativePathRequired},
		{sm.DirectoryRequest{SourceID: directoryID(root), Path: child + sep}, core.DirectoryRelativePathRequired},
		{sm.DirectoryRequest{Path: filepath.Join(root, "missing") + sep}, core.DirectoryUnavailable},
		{sm.DirectoryRequest{Path: file + sep}, core.DirectoryRequired},
		{sm.DirectoryRequest{Path: link + sep}, core.DirectorySymlinkProhibited},
		{sm.DirectoryRequest{Path: private + sep}, core.DirectoryProtected},
	} {
		_, err := resolveSandboxDirectory(t.Context(), ad, tc.request)
		if core.ReasonOf(err) != tc.reason {
			t.Fatalf("request %+v: got %v (%s), want %s", tc.request, err, core.ReasonOf(err), tc.reason)
		}
	}
	// Normalization must not turn a filesystem root into an empty/relative path.
	if filepath.VolumeName(root) == "" {
		_, err = resolveSandboxDirectory(t.Context(), ad, sm.DirectoryRequest{Path: "///"})
		if core.ReasonOf(err) != core.DirectoryRootProhibited {
			t.Fatalf("root accepted or misclassified: %v", err)
		}
	}
	ad.storageConfig.RestrictDirectories = true
	_, err = resolveSandboxDirectory(t.Context(), ad, sm.DirectoryRequest{Path: private + sep})
	if core.ReasonOf(err) != core.DirectoryOutsideScope {
		t.Fatalf("outside scope accepted or misclassified: %v", err)
	}
}

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
	// Other filesystem users may change disk usage between reference lookups.
	need(t, os.WriteFile(filepath.Join(child, "capacity-change"), make([]byte, 1<<20), 0600))
	resolved, err := resolveSandboxDirectory(t.Context(), ad, ref)
	need(t, err)
	// Capacity is a live filesystem snapshot, not part of the saved binding.
	if resolved.ID != d.ID || resolved.Path != d.Path || resolved.Name != d.Name || resolved.ReadOnly != d.ReadOnly {
		t.Fatalf("saved reference changed: before=%+v after=%+v", d, resolved)
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
	b := &remoteSandboxBackend{cfg: config.Config{DataDir: private, Sandbox: ad.storageConfig}, files: newResourceFileTools(resourceFileOptions{adapters: ad})}
	d, err := b.Directory(t.Context(), sm.DirectoryRequest{Path: root + string(filepath.Separator)})
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
