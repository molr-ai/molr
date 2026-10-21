package sandbox

import (
	"context"
	"errors"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
)

type windowsTerminalOutput struct {
	mu   sync.Mutex
	text string
}

func (o *windowsTerminalOutput) Write(data []byte) (int, error) {
	o.mu.Lock()
	o.text += string(data)
	o.mu.Unlock()
	return len(data), nil
}
func (o *windowsTerminalOutput) snapshot() string { o.mu.Lock(); defer o.mu.Unlock(); return o.text }
func (o *windowsTerminalOutput) wait(t *testing.T, text string) {
	t.Helper()
	end := time.Now().Add(15 * time.Second)
	for time.Now().Before(end) {
		if strings.Contains(o.snapshot(), text) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("missing %q; output=%q", text, o.snapshot())
}
func windowsTerminalRunner(t *testing.T) *Runner {
	t.Helper()
	helper := os.Getenv("MOLR_TEST_BINARY")
	if helper == "" {
		t.Skip("native Windows Runtime required")
	}
	private := windowsTestDirectory(t)
	if err := platform.PrivateDirectory(private); err != nil {
		t.Fatal(err)
	}
	r := New(Options{PrivateRoot: private, Helper: helper, AllowFullAccess: true, QuotaStore: quotaTestStore(t), Authorize: func(context.Context, core.Execution, platform.Command) error { return nil }})
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := r.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	caps, err := r.Capabilities(t.Context())
	if err != nil || !caps.Terminal {
		t.Fatal(caps, err, r.terminalProbeErr)
	}
	return r
}
func startWindowsTerminal(t *testing.T, r *Runner, e core.Execution, mounts []platform.Mount) (platform.Process, *windowsTerminalOutput) {
	t.Helper()
	p, err := r.Start(t.Context(), e, platform.Command{TTY: true, Managed: true, Script: " ", Directory: "/work", Mounts: mounts, MaxOutputBytes: 128 << 10, RequireIsolation: true})
	if err != nil {
		t.Fatal(err)
	}
	output := &windowsTerminalOutput{}
	go io.Copy(output, p.Stdout())
	go io.Copy(output, p.Stderr())
	return p, output
}
func writeWindowsTerminal(t *testing.T, r *Runner, e core.Execution, data string) {
	t.Helper()
	if err := r.Terminal(t.Context(), e, data, 0, 0); err != nil {
		t.Fatal(err)
	}
}
func TestWindowsInteractiveTerminalIsolationAndConcurrentSessions(t *testing.T) {
	r := windowsTerminalRunner(t)
	readonly := windowsTestDirectory(t)
	file := filepath.Join(readonly, "read.txt")
	if err := os.WriteFile(file, []byte("read"), 0600); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(r.options.PrivateRoot, "private.txt")
	if err := os.WriteFile(secret, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	e := testExecution()
	e.OperationID = "terminal-one"
	mounts := []platform.Mount{{Source: readonly, Destination: "/resources/input", ReadOnly: true}}
	first, one := startWindowsTerminal(t, r, e, mounts)
	writeWindowsTerminal(t, r, e, "Write-Output ('one'+'-ready')\r")
	one.wait(t, "one-ready")
	secondExec := e
	secondExec.OperationID = "terminal-two"
	second, two := startWindowsTerminal(t, r, secondExec, mounts)
	writeWindowsTerminal(t, r, secondExec, "Write-Output ('two'+'-ready')\r")
	two.wait(t, "two-ready")
	writeWindowsTerminal(t, r, e, "Write-Output ('unicode-' + [char]0x4e2d + [char]0x6587 + '-ok')\r")
	one.wait(t, "unicode-中文-ok")
	if err := r.Terminal(t.Context(), e, "", 35, 100); err != nil {
		t.Fatal(err)
	}
	writeWindowsTerminal(t, r, e, "Write-Output ('size-' + [Console]::WindowWidth + 'x' + [Console]::WindowHeight)\r")
	one.wait(t, "size-100x35")
	writeWindowsTerminal(t, r, e, "[IO.File]::WriteAllText((Join-Path $env:MOLR_WORKSPACE 'shared.txt'),'shared'); Write-Output ('write'+'-ok')\r")
	one.wait(t, "write-ok")
	writeWindowsTerminal(t, r, secondExec, "Write-Output ('shared-' + [IO.File]::ReadAllText((Join-Path $env:MOLR_WORKSPACE 'shared.txt')))\r")
	two.wait(t, "shared-shared")
	writeWindowsTerminal(t, r, e, "try{[IO.File]::ReadAllText("+psLiteral(secret)+");throw 'private readable'}catch [UnauthorizedAccessException]{Write-Output ('private'+'-denied')}\r")
	one.wait(t, "private-denied")
	writeWindowsTerminal(t, r, e, "try{[IO.File]::WriteAllText("+psLiteral(file)+",'bad');throw 'readonly writable'}catch [UnauthorizedAccessException]{Write-Output ('readonly'+'-denied')}\r")
	one.wait(t, "readonly-denied")
	// A simultaneous writer to the same scope must not upgrade this token.
	writerExec := e
	writerExec.OperationID = "scope-writer"
	writer, err := r.Start(t.Context(), writerExec, platform.Command{Script: "[IO.File]::WriteAllText(" + psLiteral(file) + ",'writer'); Start-Sleep -Seconds 120", Managed: true, Directory: "/work", RequireIsolation: true, MaxOutputBytes: 4096, Mounts: []platform.Mount{{Source: readonly, Destination: "/resources/input"}}})
	if err != nil {
		t.Fatal(err)
	}
	writeWindowsTerminal(t, r, e, "try{[IO.File]::WriteAllText("+psLiteral(file)+",'bad');throw 'scope upgraded'}catch [UnauthorizedAccessException]{Write-Output ('concurrent-readonly'+'-denied')}\r")
	one.wait(t, "concurrent-readonly-denied")
	if err := writer.Cancel(t.Context()); err != nil {
		t.Fatal(err)
	}
	writeWindowsTerminal(t, r, e, "$s=[Net.Sockets.Socket]::new([Net.Sockets.AddressFamily]::InterNetwork,[Net.Sockets.SocketType]::Stream,[Net.Sockets.ProtocolType]::Tcp); try{$a=$s.BeginConnect('192.0.2.1',9,$null,$null); if($a.AsyncWaitHandle.WaitOne(1000)){$s.EndConnect($a)}; Write-Output ('network'+'-allowed')}catch{Write-Output ('network-' + $_.Exception.InnerException.SocketErrorCode)}finally{$s.Dispose()}\r")
	one.wait(t, "network-AccessDenied")
	writeWindowsTerminal(t, r, e, "Write-Output ('sleep'+'-started'); Start-Sleep -Seconds 60; Write-Output ('sleep'+'-finished')\r")
	one.wait(t, "sleep-started")
	time.Sleep(300 * time.Millisecond)
	writeWindowsTerminal(t, r, e, "\x03")
	time.Sleep(300 * time.Millisecond)
	writeWindowsTerminal(t, r, e, "Write-Output ('interrupt'+'-ok')\r")
	one.wait(t, "interrupt-ok")
	if strings.Contains(one.snapshot(), "sleep-finished\r\n") {
		t.Fatal("Ctrl-C did not interrupt")
	}
	// Ending one lease must preserve the other session's access and input.
	if err := first.Cancel(t.Context()); err != nil {
		t.Fatal(err)
	}
	writeWindowsTerminal(t, r, secondExec, "Set-Content 'after.txt' 'ok'; Write-Output ('after'+'-first-stop')\r")
	two.wait(t, "after-first-stop")
	// An independent Runtime can prepare and run while this terminal stays open.
	other := windowsTerminalRunner(t)
	thirdExec := testExecution()
	thirdExec.OperationID = "independent"
	third, three := startWindowsTerminal(t, other, thirdExec, nil)
	writeWindowsTerminal(t, other, thirdExec, "Write-Output ('independent'+'-ok')\r")
	three.wait(t, "independent-ok")
	if err := third.Cancel(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := second.Cancel(t.Context()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(r.containerDirectory())
	if err != nil || len(entries) != 0 {
		t.Fatal("terminal cleanup left scope/profile journals", entries, err)
	}
}
func TestWindowsScopeLeaseSharingAndOverlap(t *testing.T) {
	private := windowsTestDirectory(t)
	root := windowsTestDirectory(t)
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	r := New(Options{PrivateRoot: private})
	if err := os.Mkdir(r.containerDirectory(), 0700); err != nil {
		t.Fatal(err)
	}
	one, err := r.acquireScope(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	two, err := r.acquireScope(t.Context(), root)
	if err != nil || one != two {
		t.Fatal("shared scope was not reused", err)
	}
	if _, err = r.acquireScope(t.Context(), child); core.ReasonOf(err) != "sandbox_file_scope_busy" {
		t.Fatal("nested scope mutation accepted", err)
	}
	other := New(Options{PrivateRoot: private})
	if _, err = other.acquireScope(t.Context(), root); core.ReasonOf(err) != "sandbox_file_scope_busy" {
		t.Fatal("other runner mutated live scope", err)
	}
	if err = r.releaseScope(one); err != nil {
		t.Fatal(err)
	}
	if _, err = other.acquireScope(t.Context(), root); core.ReasonOf(err) != "sandbox_file_scope_busy" {
		t.Fatal("scope released before last user", err)
	}
	if err = r.releaseScope(two); err != nil {
		t.Fatal(err)
	}
	next, err := other.acquireScope(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err = other.releaseScope(next); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsTerminalControlBackpressure(t *testing.T) {
	reader, writer, err := terminalControlPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.close()
	started := time.Now()
	err = writer.write(t.Context(), make([]byte, 256<<10), 100*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 2*time.Second {
		t.Fatal("unbounded terminal input", err, time.Since(started))
	}
}

func TestWindowsTerminalLifecycle(t *testing.T) {
	for _, mode := range []string{"cancel", "revoke", "timeout", "exit", "full-access"} {
		t.Run(mode, func(t *testing.T) {
			r := windowsTerminalRunner(t)
			var revoked atomic.Bool
			r.options.Authorize = func(context.Context, core.Execution, platform.Command) error {
				if revoked.Load() {
					return core.NewError(core.Forbidden, "revoked")
				}
				return nil
			}
			e := testExecution()
			e.OperationID = core.ID(mode)
			command := platform.Command{TTY: true, Managed: true, Script: " ", Directory: "/work", RequireIsolation: mode != "full-access", MaxOutputBytes: 128 << 10, FullAccess: mode == "full-access"}
			if mode == "timeout" {
				command.Timeout = 5 * time.Second
			}
			p, err := r.Start(t.Context(), e, command)
			if err != nil {
				t.Fatal(err)
			}
			output := &windowsTerminalOutput{}
			go io.Copy(output, p.Stdout())
			go io.Copy(output, p.Stderr())
			writeWindowsTerminal(t, r, e, "$child=Start-Process -FilePath (Join-Path $env:SystemRoot 'System32\\WindowsPowerShell\\v1.0\\powershell.exe') -ArgumentList '-NoLogo -NoProfile -NonInteractive -Command Start-Sleep -Seconds 120' -PassThru -NoNewWindow; [IO.File]::WriteAllText((Join-Path $env:MOLR_WORKSPACE 'child.pid'),[string]$child.Id); Write-Output ('child'+'-ready')\r")
			output.wait(t, "child-ready")
			work := platform.AssistantWorkspace(r.options.PrivateRoot, "", e.Principal.OwnerID, e.AssistantID)
			raw, err := os.ReadFile(filepath.Join(work, "child.pid"))
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
			if err != nil {
				t.Fatal(err)
			}
			child, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
			if err != nil {
				t.Fatal(err)
			}
			defer windows.CloseHandle(child)
			switch mode {
			case "revoke":
				revoked.Store(true)
			case "exit", "full-access":
				writeWindowsTerminal(t, r, e, "exit 0\r")
			case "cancel":
				if err := p.Cancel(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
			defer cancel()
			exit, err := p.Wait(ctx)
			if err != nil {
				t.Fatal(exit, err, output.snapshot())
			}
			if mode == "revoke" && exit.Reason != "authorization_changed" || mode == "timeout" && !exit.TimedOut || (mode == "exit" || mode == "full-access") && exit.Code != 0 {
				t.Fatal(exit, output.snapshot())
			}
			if state, err := windows.WaitForSingleObject(child, 0); err != nil || state != windows.WAIT_OBJECT_0 {
				t.Fatal("terminal left child alive", state, err)
			}
			if err := r.Terminal(t.Context(), e, "x", 0, 0); err == nil {
				t.Fatal("closed terminal accepted input")
			}
			entries, err := os.ReadDir(r.containerDirectory())
			if err != nil || len(entries) != 0 {
				t.Fatal("terminal cleanup left journals", entries, err)
			}
		})
	}
}

// The child is killed by the parent test to exercise real kernel Job cleanup
// and on-disk ACL recovery, without running deferred cleanup.
func TestWindowsTerminalCrashHelper(t *testing.T) {
	root := os.Getenv("MOLR_TERMINAL_CRASH_ROOT")
	if root == "" {
		t.Skip("subprocess only")
	}
	r := New(Options{PrivateRoot: root, Helper: os.Getenv("MOLR_TEST_BINARY"), QuotaStore: quotaTestStore(t), Authorize: func(context.Context, core.Execution, platform.Command) error { return nil }})
	if err := r.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	e := testExecution()
	_, out := startWindowsTerminal(t, r, e, nil)
	writeWindowsTerminal(t, r, e, "[IO.File]::WriteAllText((Join-Path $env:MOLR_WORKSPACE 'terminal.pid'),[string]$PID); Write-Output ('crash'+'-ready')\r")
	out.wait(t, "crash-ready")
	if err := os.WriteFile(filepath.Join(root, "ready"), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Minute)
}
func TestWindowsTerminalCrashRecovery(t *testing.T) {
	if os.Getenv("MOLR_TEST_BINARY") == "" {
		t.Skip("native Runtime required")
	}
	root := windowsTestDirectory(t)
	if err := platform.PrivateDirectory(root); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestWindowsTerminalCrashHelper$", "-test.timeout=2m")
	command.Env = append(os.Environ(), "MOLR_TERMINAL_CRASH_ROOT="+root)
	output := &windowsTerminalOutput{}
	command.Stdout, command.Stderr = output, output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { command.Process.Kill(); command.Wait() }()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("crash helper did not start", output.snapshot())
		}
		time.Sleep(25 * time.Millisecond)
	}
	e := testExecution()
	work := platform.AssistantWorkspace(root, "", e.Principal.OwnerID, e.AssistantID)
	raw, err := os.ReadFile(filepath.Join(work, "terminal.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	child, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(child)
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	command.Wait()
	if state, err := windows.WaitForSingleObject(child, 5000); err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatal("Runtime crash left terminal alive", state, err)
	}
	r := New(Options{PrivateRoot: root, Helper: os.Getenv("MOLR_TEST_BINARY"), QuotaStore: quotaTestStore(t), Authorize: func(context.Context, core.Execution, platform.Command) error { return nil }})
	defer r.Close()
	if err := r.Prepare(t.Context()); err != nil {
		t.Fatal("recover", err)
	}
	if caps, err := r.Capabilities(t.Context()); err != nil || !caps.Terminal {
		t.Fatal(caps, err, r.prepareErr)
	}
	entries, err := os.ReadDir(r.containerDirectory())
	if err != nil || len(entries) != 0 {
		t.Fatal("recovery journals remain", entries, err)
	}
	p, out := startWindowsTerminal(t, r, e, nil)
	writeWindowsTerminal(t, r, e, "Write-Output ('recovered'+'-ok')\r")
	out.wait(t, "recovered-ok")
	if err := p.Cancel(t.Context()); err != nil {
		t.Fatal(err)
	}
}
