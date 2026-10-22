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

// Functional checks include cold PowerShell/AppContainer startup on CI.
// These assertions verify file access and authorization, not startup latency.
const windowsCommandTestTimeout = 30 * time.Second
const windowsBackendTestTimeout = 2 * time.Minute

func windowsTestDirectory(t *testing.T) string {
	t.Helper()
	// Windows TEMP may use an 8.3 alias; fixtures need the canonical path
	// required by the production scope checks.
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return directory
}

func TestWindowsNativeBackend(t *testing.T) {
	helper := os.Getenv("MOLR_TEST_BINARY")
	if helper == "" {
		t.Skip("set MOLR_TEST_BINARY to the native Windows Runtime for enforcement acceptance")
	}
	t.Setenv("MOLR_TEST_HOST_SECRET", "host-only")
	private := windowsTestDirectory(t)
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
	ctx, cancel := context.WithTimeout(t.Context(), windowsBackendTestTimeout)
	defer cancel()
	if err := r.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	caps, err := r.Capabilities(ctx)
	if err != nil || !caps.ControlledReady() || caps.NetworkBackend != "appcontainer-toggle" || !caps.FullAccess || caps.ProcessIsolation || caps.ResourceLimits {
		t.Fatal(caps, err, "prepare:", r.prepareErr)
	}
	e := testExecution()
	existingWork := platform.AssistantWorkspace(private, "", e.Principal.OwnerID, e.AssistantID)
	if err = platform.EnsureWorkspace(existingWork); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(existingWork, "existing.txt"), []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	command := platform.Command{Script: `$ErrorActionPreference='Stop'; Set-Content -LiteralPath 'relative.txt' -Value 'relative-ok' -NoNewline; if($env:MOLR_TEST_HOST_SECRET){exit 20}; [IO.File]::Move((Join-Path $env:MOLR_WORKSPACE 'existing.txt'),(Join-Path $env:MOLR_WORKSPACE 'renamed.txt')); [IO.File]::Delete((Join-Path $env:MOLR_WORKSPACE 'renamed.txt')); [IO.File]::WriteAllText((Join-Path $env:MOLR_WORKSPACE 'output.txt'),'中文'); Write-Output '中文'; exit 0`, Directory: "/work", MaxOutputBytes: 4096, Timeout: windowsCommandTestTimeout, RequireIsolation: true}
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
	if contents, err := os.ReadFile(filepath.Join(existingWork, "relative.txt")); err != nil || string(contents) != "relative-ok" {
		t.Fatalf("PowerShell working drive: %q, %v", contents, err)
	}
	entries, err := os.ReadDir(r.containerDirectory())
	if err != nil || len(entries) != 0 {
		t.Fatal("profile journal was not cleaned", entries, err)
	}
	e.OperationID = "revoke"
	work := platform.AssistantWorkspace(private, "", e.Principal.OwnerID, e.AssistantID)
	marker := filepath.Join(work, "child.pid")
	// Leave time to observe startup and revoke authorization before the command
	// deadline; a timeout must not substitute for killing the revoked process.
	command.Timeout = 2 * windowsCommandTestTimeout
	command.Script = `$ErrorActionPreference='Stop'; $child=Start-Process -FilePath (Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe') -ArgumentList '-NoLogo -NoProfile -NonInteractive -Command Start-Sleep -Seconds 120' -PassThru -NoNewWindow; [IO.File]::WriteAllText((Join-Path $env:MOLR_WORKSPACE 'child.pid.tmp'),[string]$child.Id); [IO.File]::Move((Join-Path $env:MOLR_WORKSPACE 'child.pid.tmp'),(Join-Path $env:MOLR_WORKSPACE 'child.pid')); Start-Sleep -Seconds 120`
	proc, err = r.Start(ctx, e, command)
	if err != nil {
		t.Fatal(err)
	}
	var child windows.Handle
	deadline := time.Now().Add(windowsCommandTestTimeout)
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
	// Wait for asynchronous Job termination on the retained child handle.
	// Its 120s sleep cannot satisfy this deadline without being terminated.
	if state, err := windows.WaitForSingleObject(child, 5000); err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatal("descendant survived revocation", state, err)
	}
	entries, err = os.ReadDir(r.containerDirectory())
	if err != nil || len(entries) != 0 {
		t.Fatal("revocation left grants", entries, err)
	}
	revoked.Store(false)
	e.OperationID = "direct"
	command = platform.Command{FullAccess: true, Script: `Write-Output 'full-access'`, MaxOutputBytes: 4096, Timeout: windowsCommandTestTimeout}
	proc, err = r.Start(ctx, e, command)
	if err != nil {
		t.Fatal(err)
	}
	exit, err = proc.Wait(ctx)
	if err != nil || exit.Code != 0 {
		t.Fatal(exit, err)
	}
	for _, c := range []platform.Command{{Script: "1", Directory: "/work", Timeout: time.Second, MaxOutputBytes: 4096, Limits: platform.Limits{MemoryBytes: 1 << 30}}} {
		if _, err = r.Start(ctx, e, c); err == nil {
			t.Fatal("unsupported execution request accepted")
		}
	}
}
func TestWindowsScopeGuards(t *testing.T) {
	root := windowsTestDirectory(t)
	outside := windowsTestDirectory(t)
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
	private := windowsTestDirectory(t)
	external := windowsTestDirectory(t)
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
	private := windowsTestDirectory(t)
	resource := windowsTestDirectory(t)
	if err := platform.PrivateDirectory(private); err != nil {
		t.Fatal(err)
	}
	r := New(Options{NoWorkspace: true, PrivateRoot: private, Helper: helper, Authorize: func(context.Context, core.Execution, platform.Command) error { return nil }})
	defer r.Close()
	ctx, cancel := context.WithTimeout(t.Context(), windowsBackendTestTimeout)
	defer cancel()
	if err := r.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	for index, directory := range []string{"/tmp", "/resources/input"} {
		e := testExecution()
		e.OperationID = core.ID("remote-" + strconv.Itoa(index))
		c := platform.Command{Script: `$ErrorActionPreference='Stop'; if($env:MOLR_WORKSPACE){exit 10}; [IO.File]::WriteAllText((Join-Path (Get-Location).ProviderPath 'result.txt'),'ok'); exit 0`, Directory: directory, Timeout: windowsCommandTestTimeout, MaxOutputBytes: 4096, RequireIsolation: true, Mounts: []platform.Mount{{Source: resource, Destination: "/resources/input"}}}
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
