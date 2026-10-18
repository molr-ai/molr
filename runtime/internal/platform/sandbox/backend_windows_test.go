package sandbox

import (
	"context"
	"golang.org/x/sys/windows"
	"io"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWindowsNativeBackend(t *testing.T) {
	helper := os.Getenv("MOLR_TEST_BINARY")
	if helper == "" {
		t.Skip("set MOLR_TEST_BINARY to the native Windows Runtime for enforcement acceptance")
	}
	private := t.TempDir()
	if err := platform.PrivateDirectory(private); err != nil {
		t.Fatal(err)
	}
	var revoked atomic.Bool
	r := New(Options{QuotaStore: quotaTestStore(t), PrivateRoot: private, Helper: helper, AllowFullAccess: true, Authorize: func(context.Context, core.Execution, platform.Command) error {
		if revoked.Load() {
			return core.NewError(core.Forbidden, "revoked")
		}
		return nil
	}})
	defer r.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	lease, err := acquireContainerLease()
	if err != nil {
		t.Fatal(err)
	}
	probeErr := r.Prepare(ctx)
	lease()
	if core.ReasonOf(probeErr) != "sandbox_file_scope_busy" {
		t.Fatal("busy startup should remain retryable", probeErr)
	}
	// Capabilities retries a transient busy preparation after the owner releases it.

	caps, err := r.Capabilities(ctx)
	if err != nil || !caps.ControlledReady() || caps.NetworkBackend != "appcontainer-toggle" || !caps.FullAccess || caps.ProcessIsolation || caps.ResourceLimits {
		t.Fatal(caps, err)
	}
	e := testExecution()
	existingWork := platform.AssistantWorkspace(private, "", e.Principal.OwnerID, e.AssistantID)
	if err = platform.EnsureWorkspace(existingWork); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(existingWork, "existing.txt"), []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	command := platform.Command{Script: `[IO.File]::Move((Join-Path $env:MOLR_WORKSPACE 'existing.txt'),(Join-Path $env:MOLR_WORKSPACE 'renamed.txt')); [IO.File]::Delete((Join-Path $env:MOLR_WORKSPACE 'renamed.txt')); [IO.File]::WriteAllText((Join-Path $env:MOLR_WORKSPACE 'output.txt'),'中文'); Write-Output '中文'; exit 0`, Directory: "/work", MaxOutputBytes: 4096, Timeout: 10 * time.Second, RequireIsolation: true}
	proc, err := r.Start(ctx, e, command)
	if err != nil {
		t.Fatal(err)
	}
	exit, err := proc.Wait(ctx)
	if err != nil || exit.Code != 0 {
		data, _ := io.ReadAll(proc.Stderr())
		t.Fatal(exit, err, string(data))
	}
	data, _ := io.ReadAll(proc.Stdout())
	if !strings.Contains(string(data), "中文") {
		t.Fatal(string(data))
	}
	entries, err := os.ReadDir(r.containerDirectory())
	if err != nil || len(entries) != 0 {
		t.Fatal("profile journal was not cleaned", entries, err)
	}
	e.OperationID = "revoke"
	work := platform.AssistantWorkspace(private, "", e.Principal.OwnerID, e.AssistantID)
	marker := filepath.Join(work, "child.pid")
	command.Script = `$child=Start-Process -FilePath (Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe') -ArgumentList '-NoLogo -NoProfile -NonInteractive -Command Start-Sleep -Seconds 120' -PassThru -NoNewWindow; [IO.File]::WriteAllText((Join-Path $env:MOLR_WORKSPACE 'child.pid'),[string]$child.Id); Start-Sleep -Seconds 120`
	proc, err = r.Start(ctx, e, command)
	if err != nil {
		t.Fatal(err)
	}
	var child windows.Handle
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(marker); err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
			if err != nil {
				t.Fatal(err)
			}
			child, err = windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if child == 0 {
		t.Fatal("child startup marker missing")
	}
	defer windows.CloseHandle(child)
	if state, err := windows.WaitForSingleObject(child, 0); err != nil || state != uint32(windows.WAIT_TIMEOUT) {
		t.Fatal("child did not remain alive", state, err)
	}
	revoked.Store(true)
	exit, err = proc.Wait(ctx)
	if err != nil || exit.Reason != "authorization_changed" {
		t.Fatal(exit, err)
	}
	if state, err := windows.WaitForSingleObject(child, 0); err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatal("descendant survived revocation", state, err)
	}
	entries, err = os.ReadDir(r.containerDirectory())
	if err != nil || len(entries) != 0 {
		t.Fatal("revocation left grants", entries, err)
	}
	revoked.Store(false)
	e.OperationID = "direct"
	command = platform.Command{FullAccess: true, Script: `Write-Output 'full-access'`, MaxOutputBytes: 4096, Timeout: 10 * time.Second}
	proc, err = r.Start(ctx, e, command)
	if err != nil {
		t.Fatal(err)
	}
	exit, err = proc.Wait(ctx)
	if err != nil || exit.Code != 0 {
		t.Fatal(exit, err)
	}
	for _, c := range []platform.Command{{FullAccess: true, Script: "1", TTY: true, MaxOutputBytes: 4096}, {Script: "1", Directory: "/work", Timeout: time.Second, MaxOutputBytes: 4096, Limits: platform.Limits{MemoryBytes: 1 << 30}}} {
		if _, err = r.Start(ctx, e, c); err == nil {
			t.Fatal("unsupported execution request accepted")
		}
	}
}
func TestWindowsScopeGuards(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	r := New(Options{PrivateRoot: root})
	if _, err := r.externalScope(filepath.Dir(root)); err == nil {
		t.Fatal("private parent granted")
	}
	for _, path := range []string{`C:\`, `\\server\share`, root + ":stream"} {
		if _, err := r.externalScope(path); err == nil {
			t.Fatal(path)
		}
	}
	link := filepath.Join(root, "junction")
	if err := os.Symlink(outside, link); err == nil {
		if _, err = r.externalScope(link); err == nil {
			t.Fatal("reparse scope accepted")
		}
	}
	handles, _, err := pinScope(outside)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseScopes([][]windows.Handle{handles})
	if err := os.Rename(outside, outside+"-moved"); err == nil {
		t.Fatal("pinned scope could be renamed")
	}
}

func TestWindowsScopeHardlinksRejectedBeforeACLMutation(t *testing.T) {
	private := t.TempDir()
	external := t.TempDir()
	work := filepath.Join(private, "work")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(private, "private.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := windows.GetNamedSecurityInfo(secret, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(external, "link.txt")
	if err = os.Link(secret, link); err != nil {
		t.Fatal(err)
	}
	r := New(Options{PrivateRoot: private})
	if err = os.Mkdir(r.containerDirectory(), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(windowsShell())
	cmd.Dir = work
	if cleanup, err := r.configureContainer(t.Context(), cmd, work, platform.Command{Script: "exit 0", Mounts: []platform.Mount{{Source: external, Destination: "/resources/input", ReadOnly: true}}}); err == nil {
		cleanup()
		t.Fatal("hard-linked scope accepted")
	}
	after, err := windows.GetNamedSecurityInfo(secret, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil || before.String() != after.String() {
		t.Fatal("private hard-link ACL changed", err)
	}
	if err = os.Remove(link); err != nil {
		t.Fatal(err)
	}
	// Ordinary scopes can be granted and revoked; no child handles remain locked.
	cleanup, err := r.configureContainer(t.Context(), cmd, work, platform.Command{Script: "exit 0", Mounts: []platform.Mount{{Source: external, Destination: "/resources/input", ReadOnly: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = cleanup(); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsRemoteDirectoryAliases(t *testing.T) {
	helper := os.Getenv("MOLR_TEST_BINARY")
	if helper == "" {
		t.Skip("native Windows Runtime required")
	}
	private := t.TempDir()
	resource := t.TempDir()
	if err := platform.PrivateDirectory(private); err != nil {
		t.Fatal(err)
	}
	r := New(Options{NoWorkspace: true, PrivateRoot: private, Helper: helper, Authorize: func(context.Context, core.Execution, platform.Command) error { return nil }})
	defer r.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	if err := r.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	for index, directory := range []string{"/tmp", "/resources/input"} {
		e := testExecution()
		e.OperationID = core.ID("remote-" + strconv.Itoa(index))
		c := platform.Command{Script: `if($env:MOLR_WORKSPACE){exit 10}; [IO.File]::WriteAllText((Join-Path (Get-Location).Path 'result.txt'),'ok'); exit 0`, Directory: directory, Timeout: 10 * time.Second, MaxOutputBytes: 4096, RequireIsolation: true, Mounts: []platform.Mount{{Source: resource, Destination: "/resources/input"}}}
		proc, err := r.Start(ctx, e, c)
		if err != nil {
			t.Fatal(err)
		}
		exit, err := proc.Wait(ctx)
		if err != nil || exit.Code != 0 {
			data, _ := io.ReadAll(proc.Stderr())
			t.Fatal(exit, err, string(data))
		}
	}
	if content, err := os.ReadFile(filepath.Join(resource, "result.txt")); err != nil || string(content) != "ok" {
		t.Fatal(string(content), err)
	}
}
