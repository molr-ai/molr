//go:build linux && (amd64 || arm64)

package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/sys/unix"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestHostToolsUsesRuntimeIdentity(t *testing.T) {
	err := checkHostToolsIdentity()
	wantRejected := os.Getuid() == 0 || os.Getuid() != os.Geteuid() || os.Getgid() != os.Getegid()
	if (err != nil) != wantRejected {
		t.Fatalf("unexpected identity result: %v", err)
	}
	if wantRejected {
		caps, err := New(Options{QuotaStore: quotaTestStore(t), HostTools: true}).Capabilities(t.Context())
		if err != nil || caps.Shell || caps.Reason != "sandbox_host_tools_identity_required" {
			t.Fatalf("identity bypass: %+v %v", caps, err)
		}
	}
}
func hostToolsRunner(t *testing.T) *Runner {
	t.Helper()
	enabled := os.Getenv("MOLR_SANDBOX_HOST_TOOLS") == "1"
	if os.Getenv("MOLR_SANDBOX_INTEGRATION") != "1" || !enabled {
		t.Skip("requires host tools and real sandbox")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(exe, 0755); err != nil {
		t.Fatal(err)
	}
	r := New(Options{QuotaStore: quotaTestStore(t), HostTools: true, Helper: exe, PrivateRoot: t.TempDir(), CgroupRoot: os.Getenv("MOLR_SANDBOX_CGROUP"), MinFreeDiskBytes: 1, Authorize: func(context.Context, core.Execution, platform.Command) error { return nil }})
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := r.Prepare(t.Context()); err != nil {
		t.Fatalf("prepare: %s: %v", core.ReasonOf(err), err)
	}
	return r
}
func TestLinuxHostToolsBoundaries(t *testing.T) {
	r := hostToolsRunner(t)
	c := testCommand()
	c.Timeout = 15 * time.Second
	c.Limits.MemoryBytes = 512 << 20
	c.Limits.Processes = 64
	c.Script = `set -eu
python3 - <<'PY'
import os,socket,ctypes,errno
s=open('/proc/self/status').read()
assert 'NoNewPrivs:\t1' in s
assert 'CapEff:\t0000000000000000' in s
for family in (socket.AF_NETLINK,socket.AF_VSOCK):
 try: socket.socket(family,socket.SOCK_STREAM)
 except OSError as e: assert e.errno==errno.EPERM
 else: raise AssertionError('socket permitted')
a,b=socket.socketpair();a.send(b'ok');assert b.recv(2)==b'ok';a.close();b.close()
try: socket.socketpair(type=socket.SOCK_DGRAM)
except OSError as e: assert e.errno==errno.EPERM
else: raise AssertionError('datagram pair permitted')
PY
if touch /usr/molr-write-probe 2>/dev/null; then exit 31; fi
if sudo -n /usr/bin/id 2>/dev/null; then exit 32; fi
test ! -e /etc/shadow
test ! -e /run/docker.sock
test ! -e /home
echo host-tools-boundaries-ok
`
	if out := assertNetworkRun(t, r, c); !strings.Contains(out, "host-tools-boundaries-ok") {
		t.Fatal(out)
	}
	// Pinning public program files retains the same inode: no copied tool tree.
	var host unix.Stat_t
	if err := unix.Stat("/usr/bin/python3.12", &host); err != nil {
		t.Skip("host Python version differs")
	}
	c.Script = "stat -c %i /usr/bin/python3.12"
	if out := assertNetworkRun(t, r, c); strings.TrimSpace(out) != strconv.FormatUint(host.Ino, 10) {
		t.Fatal(out)
	}
}
func TestHostToolsDoesNotCopyPrograms(t *testing.T) {
	r := hostToolsRunner(t)
	// The managed rootfs remains the tiny BusyBox skeleton; /usr is mounted later.
	if _, err := os.Stat(filepath.Join(r.options.RootFS, "usr/bin/python3")); !os.IsNotExist(err) {
		t.Fatal("host program copied into rootfs")
	}
}

// Validate mount layout independently of account provisioning. This does NOT
// stand in for the installer's cross-UID private-file checks.
func TestLinuxHostToolsMountLayout(t *testing.T) {
	if os.Getenv("MOLR_SANDBOX_INTEGRATION") != "1" {
		t.Skip("requires real sandbox")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root, err := prepareRootFS(t.Context(), t.TempDir(), exe)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"--unshare-all", "--die-with-parent", "--new-session", "--cap-drop", "ALL", "--ro-bind", root, "/"}
	var files []*os.File
	defer func() {
		for _, f := range files {
			f.Close()
		}
	}()
	add := func(f *os.File) string { files = append(files, f); return fmt.Sprint(2 + len(files)) }
	if err = appendHostToolsMounts(&args, add); err != nil {
		t.Fatal(err)
	}
	filter, err := memoryFile(filterBytes())
	if err != nil {
		t.Fatal(err)
	}
	args = append(args, "--seccomp", add(filter), "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/work", "--tmpfs", "/tmp", "--clearenv", "--setenv", "PATH", "/usr/bin:/bin", "--setenv", "HOME", "/work", "--chdir", "/work", "--", "/bin/sh", "-c", `set -eu; python3 -c 'import ssl,sqlite3,json'; curl -V; convert -size 2x2 xc:white /work/test.png; identify /work/test.png; git init -q /work/repo; if touch /usr/molr-layout-write-test 2>/dev/null; then exit 41; fi; echo host-mount-layout-ok`)
	c := testCommand()
	c.Limits.MemoryBytes = 512 << 20
	c.Limits.Processes = 64
	group, err := newGroup(os.Getenv("MOLR_SANDBOX_CGROUP"), c.Limits)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		group.kill()
		if err := group.cleanup(); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/bwrap", args...)
	cmd.ExtraFiles = files
	cmd.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: int(group.file.Fd())}
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "host-mount-layout-ok") {
		t.Fatalf("%s %v", out, err)
	}
}

// The Runtime user creates these harmless fixtures in an isolated public
// temporary directory. The test runs as molr-sandbox, with no chown or root.
func TestLinuxHostToolsPrivateFiles(t *testing.T) {
	r := hostToolsRunner(t)
	source := os.Getenv("MOLR_SANDBOX_PERMISSION_FIXTURES")
	if source == "" {
		t.Skip("requires cross-UID permission fixtures")
	}
	var st unix.Stat_t
	if err := unix.Stat(filepath.Join(source, "private"), &st); err != nil {
		t.Fatal(err)
	}
	if st.Uid == uint32(os.Getuid()) || st.Mode&0777 != 0600 {
		t.Fatal("fixture must be a different UID's 0600 file")
	}
	c := testCommand()
	c.Timeout = 5 * time.Second
	c.Mounts = []platform.Mount{{Source: source, Destination: "/resources/probe", ReadOnly: true}}
	c.Script = `set -eu; test "$(cat /resources/probe/public)" = permission-probe; if cat /resources/probe/private >/dev/null 2>&1; then exit 41; fi; if cat /resources/probe/private-directory/public >/dev/null 2>&1; then exit 42; fi; if touch /resources/probe/public 2>/dev/null; then exit 43; fi; echo private-files-denied`
	if out := assertNetworkRun(t, r, c); !strings.Contains(out, "private-files-denied") {
		t.Fatal(out)
	}
}

// Run each shell directly under the helper, so another shell cannot repair
// the initial process group before the shell under test observes it.
func TestLinuxHostToolsTTYMountLayout(t *testing.T) {
	for _, shell := range []string{"busybox", "dash", "bash"} {
		t.Run(shell, func(t *testing.T) {
			runHostToolsTTY(t, shell, "less /work/input", func(terminal *ttyTestTerminal) {
				terminal.wait("tty-host-tools-ok")
				terminal.write("q")
			})
		})
	}
}

func runHostToolsTTY(t *testing.T, shell, script string, interact func(*ttyTestTerminal)) {
	runHostToolsTTYFixture(t, hostTTYFixture{shell: shell, script: script}, interact)
}

type hostTTYFixture struct {
	shell, script string
	hostTools     bool
	systemShells  map[string]string
	commands      map[string]string
}

func runHostToolsTTYFixture(t *testing.T, fixture hostTTYFixture, interact func(*ttyTestTerminal)) {
	if os.Getenv("MOLR_SANDBOX_INTEGRATION") != "1" {
		t.Skip("requires real sandbox")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root, err := prepareRootFS(t.Context(), t.TempDir(), exe)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"--unshare-all", "--die-with-parent", "--cap-drop", "ALL", "--ro-bind", root, "/"}
	var files []*os.File
	defer func() {
		for _, f := range files {
			f.Close()
		}
	}()
	add := func(f *os.File) string { files = append(files, f); return fmt.Sprint(2 + len(files)) }
	if err = appendHostToolsMounts(&args, add); err != nil {
		t.Fatal(err)
	}
	args = append(args, "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/work", "--tmpfs", "/tmp", "--tmpfs", "/molr")
	bind := func(source, destination string, directory bool) {
		t.Helper()
		f, err := pin(source, directory)
		if err != nil {
			t.Fatal(err)
		}
		args = append(args, "--ro-bind-fd", add(f), destination)
	}
	bind(exe, "/molr/helper", false)
	shellPath := filepath.Join(root, "bin/busybox")
	if fixture.shell == "probe" {
		shellPath = exe
	} else if fixture.shell != "busybox" && !fixture.hostTools {
		shellPath, err = filepath.EvalSymlinks("/bin/" + fixture.shell)
		if err != nil {
			t.Fatal(err)
		}
	}
	if fixture.hostTools {
		bind(shellPath, "/molr/busybox", false)
		for _, directory := range []string{"bin", "probe"} {
			bind(filepath.Join(root, "molr", directory), "/molr/"+directory, true)
		}
		if fixture.systemShells != nil {
			directory := t.TempDir()
			for name, target := range fixture.systemShells {
				if err := os.Symlink(target, filepath.Join(directory, name)); err != nil {
					t.Fatal(err)
				}
			}
			bind(directory, "/bin", true)
		}
	} else {
		bind(shellPath, "/bin/sh", false)
	}
	for target, script := range fixture.commands {
		source := filepath.Join(t.TempDir(), "command")
		if err := os.WriteFile(source, []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
		bind(source, target, false)
	}
	data, _ := json.Marshal(helperConfig{TTY: true, HostTools: fixture.hostTools, Script: fixture.script, Directory: "/work", MaxFileBytes: 64 << 20, CPUPercent: 100})
	for dst, content := range map[string][]byte{"/molr/launch.json": data, "/work/input": []byte("tty-host-tools-ok\n"), "/work/.profile": []byte("export MOLR_PROFILE_MARK=profile_loaded\n")} {
		f, e := memoryFile(content)
		if e != nil {
			t.Fatal(e)
		}
		args = append(args, "--ro-bind-data", add(f), dst)
	}
	filter, err := memoryFile(filterBytes())
	if err != nil {
		t.Fatal(err)
	}
	args = append(args, "--seccomp", add(filter), "--remount-ro", "/molr", "--clearenv", "--chdir", "/work", "--", "/molr/helper", "sandbox-helper", "supervise")
	c := testCommand()
	c.Limits.MemoryBytes = 512 << 20
	c.Limits.Processes = 64
	group, err := newGroup(os.Getenv("MOLR_SANDBOX_CGROUP"), c.Limits)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		group.kill()
		if err := group.cleanup(); err != nil {
			t.Error(err)
		}
	}()
	master, slave, err := openPTY()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/bwrap", args...)
	cmd.ExtraFiles = files
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: int(group.file.Fd()), Setsid: true, Setctty: true, Ctty: 0}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	slave.Close()
	defer func() {
		if cmd.ProcessState == nil {
			group.kill()
			cmd.Wait()
		}
	}()
	terminal := newTTYTestTerminal(t, master, ctx)
	interact(terminal)
	err = cmd.Wait()
	select {
	case <-terminal.done:
	case <-ctx.Done():
		t.Fatalf("TTY output remained open after command exit: %s", terminal.output())
	}
	out := terminal.output()
	if err != nil {
		t.Fatalf("TTY command: %v\n%s", err, out)
	}
	for _, failure := range []string{"Cannot set tty process group", "can't access tty", "no job control", "cannot set terminal process group", "job control turned off"} {
		if strings.Contains(out, failure) {
			t.Fatalf("TTY regression: %s", out)
		}
	}
}

func TestHostToolsProtectsPrivateMounts(t *testing.T) {
	var files []*os.File
	defer func() {
		for _, f := range files {
			f.Close()
		}
	}()
	add := func(f *os.File) string { files = append(files, f); return fmt.Sprint(2 + len(files)) }
	for _, path := range []string{"/usr/local/molr-private", "/usr/share/molr-work"} {
		var args []string
		err := appendHostToolsMounts(&args, add, path)
		if err == nil || core.ReasonOf(err) != "sandbox_host_tools_private_overlap" {
			t.Fatalf("private directory exposed: %s %v", path, err)
		}
	}
}
func TestLinuxHostToolsCurrentAccountPermissions(t *testing.T) {
	if os.Getenv("MOLR_SANDBOX_HOST_TOOLS") != "1" {
		t.Skip("current-account integration mode required")
	}
	r := hostToolsRunner(t)
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "private"), []byte("same-account-readable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := testCommand()
	c.Timeout = 5 * time.Second
	c.Mounts = []platform.Mount{{Source: source, Destination: "/resources/current-user", ReadOnly: true}}
	c.Script = "set -eu; cat /resources/current-user/private; if touch /resources/current-user/private 2>/dev/null; then exit 41; fi; test ! -e /home"
	if out := assertNetworkRun(t, r, c); out != "same-account-readable\n" {
		t.Fatal(out)
	}
}
