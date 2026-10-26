package sandbox

import (
	"context"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func nativeRunner(t *testing.T, auth func(context.Context, core.Execution, platform.Command) error) *Runner {
	t.Helper()
	if auth == nil {
		auth = func(context.Context, core.Execution, platform.Command) error { return nil }
	}
	r := New(Options{PrivateRoot: t.TempDir(), ToolDirectories: []string{"/opt/homebrew", "/Library/Developer/CommandLineTools"}, Authorize: auth, AllowFullAccess: true})
	if err := r.Prepare(t.Context()); err != nil {
		t.Fatalf("prepare: %v (%s)", err, core.ReasonOf(err))
	}
	t.Cleanup(func() { r.Close() })
	return r
}
func nativeCommand(script string) platform.Command {
	return platform.Command{Script: script, Directory: "/work", Timeout: 5 * time.Second, MaxOutputBytes: 8192}
}
func nativeRun(t *testing.T, r *Runner, c platform.Command) (platform.Exit, string) {
	t.Helper()
	p, err := r.Start(t.Context(), testExecution(), c)
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr []byte
	done := make(chan struct{})
	go func() { out, _ = io.ReadAll(p.Stdout()); close(done) }()
	errDone := make(chan struct{})
	go func() { stderr, _ = io.ReadAll(p.Stderr()); close(errDone) }()
	exit, err := p.Wait(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	<-done
	<-errDone
	return exit, string(out) + string(stderr)
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func TestSeatbeltNativeFilesAndEnvironment(t *testing.T) {
	r := nativeRunner(t, nil)
	caps, err := r.Capabilities(t.Context())
	if err != nil || !caps.Shell || !caps.FileIsolation || !caps.NetworkIsolation || caps.ProcessIsolation || caps.ResourceLimits || !caps.FullAccess {
		t.Fatalf("%+v %v", caps, err)
	}
	secret := filepath.Join(r.options.PrivateRoot, "private-secret")
	os.WriteFile(secret, []byte("SECRET"), 0600)
	ro := t.TempDir()
	os.WriteFile(filepath.Join(ro, "data"), []byte("RO"), 0600)
	global := platform.UserGlobal(r.options.PrivateRoot, "owner")
	os.MkdirAll(global, 0700)
	os.WriteFile(filepath.Join(global, "shared"), []byte("GLOBAL"), 0600)
	work := platform.AssistantWorkspace(r.options.PrivateRoot, "", "owner", "assistant")
	os.MkdirAll(work, 0700)
	os.Symlink(secret, filepath.Join(work, "escape"))
	t.Setenv("MOLR_SENTINEL_SECRET", "PRIVATE_ENV")
	t.Setenv("DYLD_INSERT_LIBRARIES", "/forbidden/library")
	c := nativeCommand(`test -z "$MOLR_SENTINEL_SECRET" && test -z "$DYLD_INSERT_LIBRARIES" || exit 1; echo ok > "$MOLR_WORKSPACE/result"; echo temp > "$TMPDIR/result"; cat "$MOLR_GLOBAL/shared"; if echo bad > "$MOLR_GLOBAL/shared"; then exit 2; fi; if cat "$MOLR_WORKSPACE/escape"; then exit 3; fi; if cat ` + shellQuote(secret) + `; then exit 4; fi; cat ` + shellQuote(filepath.Join(ro, "data")) + `; if echo bad > ` + shellQuote(filepath.Join(ro, "data")) + `; then exit 5; fi; echo PASSED`)
	c.Mounts = []platform.Mount{{Source: ro, Destination: "/resources/read", ReadOnly: true}}
	for _, online := range []bool{false, true} {
		c.NetworkAccess = online
		exit, out := nativeRun(t, r, c)
		if exit.Code != 0 || !strings.Contains(out, "PASSED") || strings.Contains(out, "SECRET") {
			t.Fatalf("%+v %s", exit, out)
		}
	}
	alias := filepath.Join(t.TempDir(), "swapped")
	if err := os.Symlink(ro, alias); err != nil {
		t.Fatal(err)
	}
	bad := nativeCommand("true")
	bad.Mounts = []platform.Mount{{Source: alias, ReadOnly: true}}
	if _, err := r.Start(t.Context(), testExecution(), bad); err == nil {
		t.Fatal("symlink resource scope expanded")
	}
	c = nativeCommand("echo deny")
	c.Mounts = []platform.Mount{{Source: r.options.PrivateRoot, ReadOnly: true}}
	if _, err := r.Start(t.Context(), testExecution(), c); err == nil {
		t.Fatal("private root accepted")
	}
}

// The test binary doubles as a native socket/sysctl probe; only its own executable is readable.
func TestSeatbeltChild(t *testing.T) {
	args := os.Args
	at := -1
	for i, a := range args {
		if a == "--" {
			at = i
			break
		}
	}
	if at < 0 {
		return
	}
	args = args[at+1:]
	var err error
	switch args[0] {
	case "tcp":
		var l net.Listener
		l, err = net.Listen("tcp", args[1])
		if err == nil {
			l.Close()
		}
	case "udp":
		var c net.PacketConn
		c, err = net.ListenPacket("udp", args[1])
		if err == nil {
			c.Close()
		}
	case "unix":
		var c net.Conn
		c, err = net.Dial("unix", args[1])
		if err == nil {
			c.Close()
		}
	case "fd":
		fd, _ := strconv.Atoi(args[1])
		_, err = unix.Read(fd, make([]byte, 32))
	case "proc":
		pid, _ := strconv.Atoi(args[1])
		_, err = unix.SysctlRaw("kern.procargs2", pid)
	default:
		os.Exit(2)
	}
	if err != nil {
		fmt.Println(err)
		os.Exit(77)
	}
	fmt.Println("ALLOWED")
	os.Exit(0)
}
func TestSeatbeltNativeNetworkAndProcessInfo(t *testing.T) {
	r := nativeRunner(t, nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "CONNECTED") }))
	defer server.Close()
	for _, online := range []bool{false, true} {
		c := nativeCommand("curl --noproxy '*' --max-time 2 --silent " + shellQuote(server.URL))
		c.NetworkAccess = online
		exit, out := nativeRun(t, r, c)
		if (exit.Code == 0) != online || strings.Contains(out, "CONNECTED") != online {
			t.Fatalf("online=%v %+v %s", online, exit, out)
		}
	}
	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	socketDir, err := os.MkdirTemp("/tmp", "molr-uds-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(socketDir)
	socketDir, _ = filepath.EvalSymlinks(socketDir)
	sock := filepath.Join(socketDir, "test.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	for _, probe := range []struct{ kind, arg string }{{"tcp", "0.0.0.0:0"}, {"tcp", "[::]:0"}, {"udp", "0.0.0.0:0"}, {"udp", "[::]:0"}, {"unix", sock}, {"proc", strconv.Itoa(os.Getpid())}} {
		for _, online := range []bool{false, true} {
			c := nativeCommand(shellQuote(exe) + " -test.run=^TestSeatbeltChild$ -- " + probe.kind + " " + shellQuote(probe.arg))
			c.NetworkAccess = online
			c.Mounts = []platform.Mount{{Source: exe, ReadOnly: true}, {Source: socketDir}}
			exit, out := nativeRun(t, r, c)
			allowed := online && (probe.kind == "tcp" || probe.kind == "udp")
			want := 77
			if allowed {
				want = 0
			}
			if exit.Code != want {
				t.Fatalf("%s online=%v %+v %s", probe.kind, online, exit, out)
			}
		}
	}
}
func TestSeatbeltNativeRevocationAndTerminal(t *testing.T) {
	var revoked atomic.Bool
	r := nativeRunner(t, func(context.Context, core.Execution, platform.Command) error {
		if revoked.Load() {
			return core.NewError(core.Forbidden, "revoked")
		}
		return nil
	})
	testTerminalRevocation(t, r, nativeCommand(""), &revoked)
}

func TestSeatbeltNativeForwardAndDeadline(t *testing.T) {
	r := nativeRunner(t, nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "FORWARDED") }))
	defer server.Close()
	_, rawPort, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	port, _ := strconv.Atoi(rawPort)
	c := nativeCommand("sleep 30")
	c.NetworkAccess = true
	c.ForwardPorts = []int{port}
	c.AuthorizationDeadline = time.Now().Add(time.Second)
	p, err := r.Start(t.Context(), testExecution(), c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.DialForward(t.Context(), testExecution(), port+1); err == nil {
		t.Fatal("undeclared port accepted")
	}
	conn, err := r.DialForward(t.Context(), testExecution(), port)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprint(conn, "GET / HTTP/1.0\r\nHost: localhost\r\n\r\n")
	data, err := io.ReadAll(conn)
	conn.Close()
	if err != nil || !strings.Contains(string(data), "FORWARDED") {
		t.Fatalf("%s %v", data, err)
	}
	// An idle managed connection is closed together with the task.
	idle, err := r.DialForward(t.Context(), testExecution(), port)
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	deadline, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	exit, err := p.Wait(deadline)
	if err != nil || !exit.TimedOut {
		t.Fatalf("%+v %v", exit, err)
	}
	idle.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := idle.Read(make([]byte, 1)); err == nil {
		t.Fatal("connection survived task")
	}
	if _, err := r.DialForward(t.Context(), testExecution(), port); err == nil {
		t.Fatal("finished task forwarded")
	}
}

func TestSeatbeltNativeHostTools(t *testing.T) {
	r := nativeRunner(t, nil)
	c := nativeCommand(`set -e; mkdir "$MOLR_WORKSPACE/repo"; cd "$MOLR_WORKSPACE/repo"; git init -q; git status --porcelain; printf '%s\n' alpha beta | sort | tail -1; python3 -c 'import ssl, pathlib; pathlib.Path("python.txt").write_text("python")'; node -e 'require("fs").writeFileSync("node.txt","node")'; echo TOOLS_OK`)
	// These optional deployment tools are present on the native acceptance host.
	for _, path := range []string{"/opt/homebrew/bin/python3", "/opt/homebrew/bin/node", "/Library/Developer/CommandLineTools/usr/bin/git"} {
		if _, err := os.Stat(path); err != nil {
			t.Skip("optional tools unavailable")
		}
	}
	exit, out := nativeRun(t, r, c)
	if exit.Code != 0 || !strings.Contains(out, "TOOLS_OK") {
		t.Fatalf("%+v %s", exit, out)
	}
}

func TestSeatbeltProtectedSystemScopeFailsClosed(t *testing.T) {
	r := New(Options{PrivateRoot: t.TempDir(), ProtectedRoots: []string{"/usr/share"}, AllowFullAccess: true})
	defer r.Close()
	if err := r.Prepare(t.Context()); core.ReasonOf(err) != "sandbox_seatbelt_protected_root" {
		t.Fatalf("%v", err)
	}
	caps, err := r.Capabilities(t.Context())
	if err != nil || caps.Shell || !caps.FullAccess {
		t.Fatalf("%+v %v", caps, err)
	}
}

func TestSeatbeltClosesUnrelatedInheritedDescriptors(t *testing.T) {
	r := nativeRunner(t, nil)
	secret, err := os.CreateTemp(r.options.PrivateRoot, "fd-secret-")
	if err != nil {
		t.Fatal(err)
	}
	defer secret.Close()
	secret.WriteString("PRIVATE_FD")
	secret.Seek(0, 0)
	if _, err = unix.FcntlInt(secret.Fd(), unix.F_SETFD, 0); err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	high, err := unix.FcntlInt(secret.Fd(), unix.F_DUPFD, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(high)
	c := nativeCommand(shellQuote(exe) + " -test.run=^TestSeatbeltChild$ -- fd " + strconv.Itoa(high))
	c.Mounts = []platform.Mount{{Source: exe, ReadOnly: true}}
	exit, out := nativeRun(t, r, c)
	if exit.Code != 77 {
		t.Fatalf("inherited descriptor accessible: %+v %s", exit, out)
	}
}

func TestSeatbeltRemoteExecutionScopes(t *testing.T) {
	r := New(Options{PrivateRoot: t.TempDir(), NoWorkspace: true, Authorize: func(context.Context, core.Execution, platform.Command) error { return nil }})
	defer r.Close()
	if err := r.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "input"), []byte("READ_OK"), 0600); err != nil {
		t.Fatal(err)
	}
	destination := "/resources/test/allowed"
	variable := platform.ResourceEnvironment(destination)
	c := nativeCommand(`test -z "$MOLR_WORKSPACE" && test -z "$MOLR_GLOBAL" || exit 1; cat "$` + variable + `/input"; if echo bad > "$` + variable + `/input"; then exit 2; fi; echo ok > "$TMPDIR/result"; printf '\nHOME=%s\nTMP=%s\n' "$HOME" "$TMPDIR"`)
	c.Directory = destination
	c.Mounts = []platform.Mount{{Source: root, Destination: destination, ReadOnly: true}}
	exit, out := nativeRun(t, r, c)
	if exit.Code != 0 || !strings.Contains(out, "READ_OK") {
		t.Fatalf("%+v %s", exit, out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "HOME=") || strings.HasPrefix(line, "TMP=") {
			_, p, _ := strings.Cut(line, "=")
			if _, err := os.Stat(p); !os.IsNotExist(err) {
				t.Fatalf("temporary execution directory survived: %s %v", p, err)
			}
		}
	}
	if _, err := os.Stat(platform.AssistantWorkspace(r.options.PrivateRoot, "", "owner", "assistant")); !os.IsNotExist(err) {
		t.Fatal("remote assistant workspace created", err)
	}
	c.Mounts[0].ReadOnly = false
	c.Script = `echo WRITE_OK > "$` + variable + `/result"`
	exit, out = nativeRun(t, r, c)
	if exit.Code != 0 {
		t.Fatalf("%+v %s", exit, out)
	}
	if data, err := os.ReadFile(filepath.Join(root, "result")); err != nil || string(data) != "WRITE_OK\n" {
		t.Fatalf("%s %v", data, err)
	}
}
