//go:build linux && (amd64 || arm64)

package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// The TTY fixture can mount this binary in the shell's position. Inspect
	// the inherited state before a real shell can repair job control itself.
	if os.Args[0] == "sh" {
		group := unix.Getpgrp()
		foreground, err := unix.IoctlGetInt(0, unix.TIOCGPGRP)
		if err != nil || group <= 0 || foreground != group {
			fmt.Fprintf(os.Stderr, "invalid initial TTY group: group=%d foreground=%d err=%v\n", group, foreground, err)
			os.Exit(1)
		}
		fmt.Println("tty-initial-group-ok")
		os.Exit(0)
	}
	if len(os.Args) > 1 && os.Args[1] == "sandbox-network-client" {
		if err := networkTestClient(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if len(os.Args) == 2 && os.Args[1] == "sandbox-supervisor" {
		os.Exit(SupervisorMain())
	}
	if len(os.Args) > 1 && os.Args[1] == "sandbox-test-syscalls" {
		for _, family := range []int{unix.AF_UNIX, unix.AF_VSOCK, unix.AF_NETLINK} {
			fd, e := unix.Socket(family, unix.SOCK_STREAM, 0)
			if e != unix.EPERM {
				if e == nil {
					unix.Close(fd)
				}
				os.Exit(1)
			}
		}
		if unix.Unshare(unix.CLONE_NEWUSER) == nil || unix.PtraceAttach(os.Getppid()) == nil {
			os.Exit(1)
		}
		fmt.Print("boundaries-ok")
		os.Exit(0)
	}
	if len(os.Args) > 1 && os.Args[1] == "sandbox-helper" {
		os.Exit(HelperMain(os.Args[2:]))
	}
	os.Exit(m.Run())
}

func TestLinuxRealSyscallsAndTempLimit(t *testing.T) {
	r := integrationRunner(t, nil)
	c := testCommand()
	c.Timeout = 10 * time.Second
	c.Script = "/molr/helper sandbox-test-syscalls"
	out, exit := executeTest(t, r, c)
	if exit.Code != 0 || out != "boundaries-ok" {
		t.Fatalf("syscall boundary: %+v %q", exit, out)
	}
	c.Script = "dd if=/dev/zero of=/tmp/large bs=1048576 count=16"
	_, exit = executeTest(t, r, c)
	if exit.Code == 0 {
		t.Fatal("temporary filesystem exceeded hard capacity")
	}
}

func TestLinuxAutomaticPreparationUsesOrdinaryDirectories(t *testing.T) {
	if os.Getenv("MOLR_SANDBOX_INTEGRATION") != "1" {
		t.Skip("real sandbox environment required")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(exe, 0755); err != nil {
		t.Fatal(err)
	}
	private := t.TempDir()
	r := New(Options{QuotaStore: quotaTestStore(t), PrivateRoot: private, Helper: exe, MinFreeDiskBytes: 1, Authorize: func(context.Context, core.Execution, platform.Command) error { return nil }})
	defer r.Close()
	if err := r.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	caps, err := r.Capabilities(t.Context())
	if err != nil || !caps.Shell || caps.DiskProtection != "file_limit_and_free_space_guard" {
		t.Fatalf("%+v %v", caps, err)
	}
	if !strings.HasPrefix(r.options.RootFS, private+"/") || !strings.HasPrefix(r.options.WorkRoot, private+"/") {
		t.Fatal("assets not managed automatically")
	}
	source := t.TempDir()
	c := testCommand()
	c.Mounts = []platform.Mount{{Source: source, Destination: "/resources/local"}}
	c.Script = "echo ordinary > /resources/local/result; echo persistent > /work/keep"
	_, exit := executeTest(t, r, c)
	if exit.Code != 0 {
		t.Fatalf("%+v", exit)
	}
	data, err := os.ReadFile(filepath.Join(source, "result"))
	if err != nil || string(data) != "ordinary\n" {
		t.Fatalf("ordinary folder write failed: %q %v", data, err)
	}
	c.Script = "cat /work/keep"
	out, exit := executeTest(t, r, c)
	if exit.Code != 0 || out != "persistent\n" {
		t.Fatalf("%+v %q", exit, out)
	}
	c.Limits.MaxFileBytes = 1024
	c.Script = "dd if=/dev/zero of=/work/large bs=1024 count=2"
	_, exit = executeTest(t, r, c)
	if exit.Code == 0 {
		t.Fatal("file limit not enforced")
	}
}

func TestLinuxAutomaticSupervisorDelegatesWithoutCallerConfiguration(t *testing.T) {
	if os.Getenv("MOLR_SANDBOX_INTEGRATION") != "1" {
		t.Skip("real sandbox environment required")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(exe, 0755); err != nil {
		t.Fatal(err)
	}
	root, closeEnvironment, err := startSupervisor(t.Context(), exe)
	if err != nil {
		t.Fatal(err)
	}
	defer closeEnvironment()
	r := New(Options{QuotaStore: quotaTestStore(t), PrivateRoot: t.TempDir(), Helper: exe, CgroupRoot: root, MinFreeDiskBytes: 1, Authorize: func(context.Context, core.Execution, platform.Command) error { return nil }})
	defer r.Close()
	if err := r.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	out, exit := executeTest(t, r, testCommand())
	if exit.Code != 0 || out != "ok\n" {
		t.Fatalf("%+v %q", exit, out)
	}
}
func TestLinuxRealDoesNotLeakHostMountDescriptors(t *testing.T) {
	r := integrationRunner(t, nil)
	c := testCommand()
	c.Timeout = 10 * time.Second
	c.Script = "for f in /proc/[0-9]*/fd/*; do /bin/busybox readlink \"$f\"; done; exit 0"
	out, exit := executeTest(t, r, c)
	if exit.Code != 0 {
		t.Fatalf("%+v", exit)
	}
	for _, secret := range []string{r.options.RootFS, r.options.WorkRoot, r.options.Helper, r.options.CgroupRoot, "memfd:molr-sandbox"} {
		if strings.Contains(out, secret) {
			t.Fatalf("host descriptor leaked: %s", out)
		}
	}
}
func TestLinuxRealExecutionScopedControl(t *testing.T) {
	r := integrationRunner(t, nil)
	c := testCommand()
	c.Timeout = 10 * time.Second
	c.Script = "sleep 8"
	e := testExecution()
	p, err := r.Start(t.Context(), e, c)
	if err != nil {
		t.Fatal(err)
	}
	foreign := e
	foreign.OperationID = "other"
	if err := r.CancelOperation(t.Context(), foreign); err != nil {
		t.Fatal(err)
	} // Missing operation is an idempotent no-op; the original must remain alive.
	if err := r.StopOwner(t.Context(), "other-owner"); err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err = p.Wait(short); err == nil {
		t.Fatal("foreign owner stopped process")
	}
	if err := r.CancelOperation(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	p, err = r.Start(t.Context(), e, c)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.StopOwner(t.Context(), e.Principal.OwnerID); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	p, err = r.Start(t.Context(), e, c)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	caps, err := r.Capabilities(t.Context())
	if err != nil || caps.Shell {
		t.Fatal("closed sandbox still available")
	}
}
func TestLinuxRealCgroupLimitsAndExpiry(t *testing.T) {
	r := integrationRunner(t, nil)
	c := testCommand()
	c.Timeout = 10 * time.Second
	c.AuthorizationDeadline = time.Now().Add(500 * time.Millisecond)
	c.Script = "sleep 5"
	p, e := r.Start(context.Background(), testExecution(), c)
	if e != nil {
		t.Fatal(e)
	}
	entries, e := os.ReadDir(r.options.CgroupRoot)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "molr-") {
			found = true
			root := filepath.Join(r.options.CgroupRoot, entry.Name())
			for name, want := range map[string]string{"memory.max": "134217728", "memory.swap.max": "0", "pids.max": "32", "cpu.max": "100000 100000"} {
				b, e := os.ReadFile(filepath.Join(root, name))
				if e != nil || strings.TrimSpace(string(b)) != want {
					t.Fatalf("%s=%s %v", name, b, e)
				}
			}
		}
	}
	if !found {
		t.Fatal("process started outside configured cgroup")
	}
	exit, e := p.Wait(t.Context())
	if e != nil || !exit.TimedOut || exit.Code != 125 {
		t.Fatalf("expiry: %+v %v", exit, e)
	}
}
func TestLinuxRealRecoversStaleCgroupAndRejectsPrivateMount(t *testing.T) {
	r := integrationRunner(t, nil)
	stale, e := newGroup(r.options.CgroupRoot, testCommand().Limits)
	if e != nil {
		t.Fatal(e)
	}
	stale.file.Close()
	c := testCommand()
	c.Script = "echo ok"
	out, exit := executeTest(t, r, c)
	if out != "ok\n" || exit.Code != 0 {
		t.Fatalf("%+v %q", exit, out)
	}
	if _, e := os.Stat(stale.path); !os.IsNotExist(e) {
		t.Fatal("stale cgroup was not recovered")
	}
	c.Mounts = []platform.Mount{{Source: r.options.PrivateRoot, Destination: "/resources/private", ReadOnly: true}}
	if _, e := r.Start(t.Context(), testExecution(), c); e == nil {
		t.Fatal("private runtime directory mounted")
	}
}
func integrationRunner(t *testing.T, auth func(context.Context, core.Execution, platform.Command) error) *Runner {
	t.Helper()
	if os.Getenv("MOLR_SANDBOX_INTEGRATION") != "1" {
		t.Skip("run scripts/test-sandbox-linux.sh for mandatory real isolation tests")
	}
	root := t.TempDir()
	for _, p := range []string{"bin", "usr/bin", "etc", "work", "global", "tmp", "proc", "dev", "resources", "molr"} {
		if e := os.MkdirAll(filepath.Join(root, p), 0755); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.WriteFile(filepath.Join(root, "etc/resolv.conf"), nil, 0644); e != nil {
		t.Fatal(e)
	}
	busy, e := os.ReadFile("/usr/bin/busybox")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(root, "bin/busybox"), busy, 0755); e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{"sh", "cat", "sleep", "touch", "rm", "mkdir", "setsid", "wget", "nc", "head", "yes", "wc", "dd", "ln", "test"} {
		if e = os.Symlink("busybox", filepath.Join(root, "bin", s)); e != nil {
			t.Fatal(e)
		}
	}
	work, e := os.MkdirTemp("/dev/shm", "molr-work-test-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(work) })
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(exe, 0755); e != nil {
		t.Fatal(e)
	}
	if auth == nil {
		auth = func(context.Context, core.Execution, platform.Command) error { return nil }
	}
	r := New(Options{QuotaStore: quotaTestStore(t), Bwrap: "/usr/bin/bwrap", RootFS: root, Helper: exe, WorkRoot: work, MinFreeDiskBytes: 1, PrivateRoot: t.TempDir(), CgroupRoot: os.Getenv("MOLR_SANDBOX_CGROUP"), Authorize: auth})
	t.Cleanup(func() {
		if e := r.Close(); e != nil {
			t.Error(e)
		}
	})
	if c, e := r.Capabilities(context.Background()); e != nil || !c.Shell {
		for _, p := range []string{r.options.Bwrap, r.options.Helper} {
			f, err := pin(p, false)
			if err != nil {
				t.Logf("pin %s: %v", p, err)
			} else {
				i, err := f.Stat()
				t.Logf("pin %s: %v %v", p, i.Mode(), err)
				f.Close()
			}
		}
		t.Fatalf("required sandbox unavailable: %+v %v", c, e)
	}
	return r
}
func executeTest(t *testing.T, r *Runner, c platform.Command) (string, platform.Exit) {
	t.Helper()
	p, e := r.Start(context.Background(), testExecution(), c)
	if e != nil {
		t.Fatal(e)
	}
	out, e := io.ReadAll(p.Stdout())
	if e != nil {
		t.Fatal(e)
	}
	stderr, _ := io.ReadAll(p.Stderr())
	exit, e := p.Wait(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if len(stderr) > 0 {
		t.Logf("stderr: %s", stderr)
	}
	return string(out), exit
}
func TestLinuxRealSandboxIsolation(t *testing.T) {
	r := integrationRunner(t, nil)
	c := testCommand()
	c.Timeout = 10 * time.Second
	c.Script = "test ! -e /home; test ! -e /run; test ! -e /sys; echo isolated; echo persistent > /work/value; cat /work/value"
	out, exit := executeTest(t, r, c)
	if exit.Code != 0 || out != "isolated\npersistent\n" {
		t.Fatalf("%+v %q", exit, out)
	}
	c.Script = "cat /work/value"
	out, exit = executeTest(t, r, c)
	if exit.Code != 0 || out != "persistent\n" {
		t.Fatalf("workspace lost: %+v %q", exit, out)
	}
}
func TestLinuxRealDirectMountAndReadonly(t *testing.T) {
	r := integrationRunner(t, nil)
	source, e := os.MkdirTemp("/dev/shm", "molr-resource-test-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(source)
	os.WriteFile(filepath.Join(source, "file"), []byte("original"), 0600)
	c := testCommand()
	c.Mounts = []platform.Mount{{Source: source, Destination: "/resources/files", ReadOnly: true}}
	c.Script = "cat /resources/files/file; echo changed > /resources/files/file"
	out, exit := executeTest(t, r, c)
	if out != "original" || exit.Code == 0 {
		t.Fatalf("readonly boundary: %+v %q", exit, out)
	}
	c.Mounts[0].ReadOnly = false

	c.Script = "echo changed > /resources/files/file"
	_, exit = executeTest(t, r, c)
	b, _ := os.ReadFile(filepath.Join(source, "file"))
	if exit.Code != 0 || string(b) != "changed\n" {
		t.Fatalf("direct modification missing: %+v %q", exit, b)
	}
}

func TestLinuxRealNestedResourcePathsAndStorageLifetimes(t *testing.T) {
	r := integrationRunner(t, nil)
	root := t.TempDir()
	for _, name := range []string{"read", "write", "secret"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name, "file"), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	c := testCommand()
	c.Mounts = []platform.Mount{{Source: filepath.Join(root, "write"), Destination: "/resources/stable/write"}, {Source: filepath.Join(root, "read"), Destination: "/resources/stable/read", ReadOnly: true}}
	c.Directory = "/resources/stable/write"
	c.Script = `set -eu
test ! -e /resources/stable/secret
test "$(cat ../read/file)" = read
echo updated > file
if (echo invalid > ../read/file) 2>/dev/null; then exit 10; fi
if (echo invalid > ../new-file) 2>/dev/null; then exit 11; fi
if (echo invalid > /new-file) 2>/dev/null; then exit 12; fi
if (echo invalid > /molr/new-file) 2>/dev/null; then exit 13; fi
echo persistent > /work/result
echo temporary > /tmp/result
echo boundaries-ok`
	out, exit := executeTest(t, r, c)
	if exit.Code != 0 || out != "boundaries-ok\n" {
		t.Fatalf("nested scope isolation: %+v %q", exit, out)
	}
	data, err := os.ReadFile(filepath.Join(root, "write/file"))
	if err != nil || string(data) != "updated\n" {
		t.Fatalf("direct write missing: %v", err)
	}
	c.Mounts[0], c.Mounts[1] = c.Mounts[1], c.Mounts[0]
	c.Script = `set -eu; test ! -e /tmp/result; test "$(cat /work/result)" = persistent; test "$(cat file)" = updated; echo lifetime-ok`
	out, exit = executeTest(t, r, c)
	if exit.Code != 0 || out != "lifetime-ok\n" {
		t.Fatalf("reordered mounts or storage lifetime failed: %+v %q", exit, out)
	}
}
func TestLinuxRealCancelKillsDetachedChildren(t *testing.T) {
	r := integrationRunner(t, nil)
	c := testCommand()
	c.Timeout = 15 * time.Second
	c.Script = "setsid sh -c 'while true; do echo tick >> /work/ticks; sleep .02; done' & sleep 10"
	p, e := r.Start(context.Background(), testExecution(), c)
	if e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(platform.AssistantWorkspace(r.options.PrivateRoot, r.options.WorkRoot, "owner", "assistant"), "ticks")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if b, _ := os.ReadFile(file); len(b) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child failed to start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e = p.Cancel(ctx); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(file)
	time.Sleep(100 * time.Millisecond)
	after, _ := os.ReadFile(file)
	if string(before) != string(after) {
		t.Fatal("detached process survived cancel")
	}
}
func TestLinuxRealRevocationAndOutputLimit(t *testing.T) {
	var revoked atomic.Bool
	r := integrationRunner(t, func(context.Context, core.Execution, platform.Command) error {
		if revoked.Load() {
			return core.NewError(core.Forbidden, "revoked")
		}
		return nil
	})
	c := testCommand()
	c.Timeout = 10 * time.Second
	c.Script = "yes output"
	p, e := r.Start(context.Background(), testExecution(), c)
	if e != nil {
		t.Fatal(e)
	}
	time.Sleep(100 * time.Millisecond)
	revoked.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	exit, e := p.Wait(ctx)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := io.ReadAll(p.Stdout())
	if len(b) > 4096 || !exit.OutputTruncated || exit.Code != 125 {
		t.Fatalf("revocation/output: %+v bytes=%d", exit, len(b))
	}
}
func TestLinuxRealTransparentNetworkAndDenied(t *testing.T) {
	r := integrationRunner(t, nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "network-ok") }))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	c := testCommand()
	c.Timeout = 10 * time.Second
	c.NetworkTargets = []platform.NetworkTarget{{Protocols: []string{"tcp"}, Addresses: []string{"10.0.0.0/8", "127.0.0.0/8"}, Ports: []string{"1-10", u.Port() + "-65535"}}}
	c.Script = "wget -qO- http://host.molr.internal:" + u.Port() + "; echo; test -z \"$HTTP_PROXY$http_proxy$HTTPS_PROXY$https_proxy\"; echo transparent:$?"
	out, exit := executeTest(t, r, c)
	if exit.Code != 0 || !strings.Contains(out, "network-ok") || strings.Contains(out, "direct:0") {
		t.Fatalf("network boundary: %+v %q", exit, out)
	}
	for _, excludeAddress := range []bool{false, true} {
		c.NetworkTargets[0].ExcludeAddresses = nil
		c.NetworkTargets[0].ExcludePorts = nil
		if excludeAddress {
			c.NetworkTargets[0].ExcludeAddresses = []string{"127.0.0.0/8"}
		} else {
			c.NetworkTargets[0].ExcludePorts = []string{u.Port() + "-65535"}
		}
		c.Script = "wget -T 1 -qO- http://host.molr.internal:" + u.Port() + "; echo denied:$?"
		out, exit = executeTest(t, r, c)
		if exit.Code != 0 || strings.Contains(out, "network-ok") || strings.Contains(out, "denied:0") {
			t.Fatalf("exclusion boundary (address=%v): %+v %q", excludeAddress, exit, out)
		}
	}
}

func TestLinuxComputeQuotaSharesParentAcrossConcurrentExecutions(t *testing.T) {
	r := integrationRunner(t, nil)
	c := testCommand()
	c.ComputeQuota = true
	c.Script = "sleep 30"
	c.Timeout = time.Minute
	first := testExecution()
	first.OperationID = "shared-first"
	second := first
	second.OperationID = "shared-second"
	p, err := r.Start(t.Context(), first, c)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Cancel(context.Background())
	q, err := r.Start(t.Context(), second, c)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Cancel(context.Background())
	sum := sha256.Sum256([]byte(string(first.Principal.OwnerID) + "/" + string(first.AssistantID)))
	parent := filepath.Join(r.options.CgroupRoot, "quota-"+hex.EncodeToString(sum[:16]))
	memory, err := os.ReadFile(filepath.Join(parent, "memory.max"))
	if err != nil || strings.TrimSpace(string(memory)) != strconv.FormatInt(c.Limits.MemoryBytes, 10) {
		t.Fatal(string(memory), err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			count++
		}
	}
	if count != 2 {
		t.Fatal("executions not sharing quota parent", count)
	}
	usage := r.ComputeUsage(t.Context(), first.Principal.OwnerID, first.AssistantID)
	if usage.Processes < 2 || usage.MemoryBytes <= 0 {
		t.Fatal(usage)
	}
	if err := r.StopAssistant(t.Context(), first.Principal.OwnerID, first.AssistantID); err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := p.Wait(wait); err != nil {
		t.Fatal("first execution survived stop", err)
	}
	if _, err := q.Wait(wait); err != nil {
		t.Fatal("second execution survived stop", err)
	}
}

func TestLinuxUserGroupComputeAcrossAssistantsAndIndependentUsers(t *testing.T) {
	r := integrationRunner(t, nil)
	c := testCommand()
	c.ComputeQuota = true
	c.Timeout = time.Minute
	c.Script = "echo private > /work/identity; sleep 30"
	c.GroupComputeConstraints = &policy.ComputeConstraints{CPUPercent: 100, MemoryBytes: 256 << 20, Processes: 64, TempBytes: 8 << 20, MaxDurationSeconds: 60}
	e := testExecution()
	for _, assistant := range []core.ID{"first", "second"} {
		e.AssistantID = assistant
		e.Principal.SubjectID = assistant
		e.OperationID = assistant
		p, err := r.Start(t.Context(), e, c)
		if err != nil {
			t.Fatal(err)
		}
		defer p.Cancel(context.Background())
		if v := r.ComputeUsage(t.Context(), e.Principal.OwnerID, assistant); v.Processes < 1 {
			t.Fatal("account-nested assistant usage missing", v)
		}
	}
	sum := sha256.Sum256([]byte(string(e.Principal.OwnerID)))
	parent := filepath.Join(r.options.CgroupRoot, "account-"+hex.EncodeToString(sum[:16]))
	memory, err := os.ReadFile(filepath.Join(parent, "memory.max"))
	if err != nil || strings.TrimSpace(string(memory)) != "268435456" {
		t.Fatal(string(memory), err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			count++
		}
	}
	if count != 2 {
		t.Fatal("assistants do not share account ceiling", count)
	}
	e.Principal.OwnerID = "another-member"
	e.OperationID = "other-member"
	c.Script = "test ! -f /work/identity && echo isolated"
	c.Timeout = 10 * time.Second
	p, err := r.Start(t.Context(), e, c)
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Wait(t.Context())
	if err != nil || result.Code != 0 {
		t.Fatal(result, err)
	}
	other := sha256.Sum256([]byte(string(e.Principal.OwnerID)))
	if _, err := os.Stat(filepath.Join(r.options.CgroupRoot, "account-"+hex.EncodeToString(other[:16]), "memory.max")); err != nil {
		t.Fatal("other account missing independent quota", err)
	}
}

func TestRemoteExecutionHasNoPersistentAssistantWorkspace(t *testing.T) {
	r := integrationRunner(t, func(context.Context, core.Execution, platform.Command) error { return nil })
	r.options.NoWorkspace = true
	c := platformProbe()
	c.Directory = "/tmp"
	c.Script = `test "$HOME" = /tmp; test ! -w /work; echo transient > /tmp/remote-only; cat /tmp/remote-only`
	output, exit := executeTest(t, r, c)
	if exit.Code != 0 || !strings.Contains(output, "transient") {
		t.Fatal(output, exit)
	}
	c.Script = `test ! -e /tmp/remote-only; test ! -w /work; echo fresh`
	output, exit = executeTest(t, r, c)
	if exit.Code != 0 || !strings.Contains(output, "fresh") {
		t.Fatal(output, exit)
	}
	c.Directory = "/work"
	if _, err := r.Start(t.Context(), probeExecution(), c); err == nil {
		t.Fatal("remote /work accepted")
	}
}
