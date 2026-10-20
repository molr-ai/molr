package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"molr.ai/runtime/internal/browser"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
)

// Set MOLR_TEST_BROWSER_AGENT, MOLR_TEST_BROWSER_CHROMIUM and MOLR_TEST_HELPER
// to run against real pinned components; no external websites or accounts used.
func TestSeatbeltNativeBrowser(t *testing.T) {
	agent, chromium, helper := os.Getenv("MOLR_TEST_BROWSER_AGENT"), os.Getenv("MOLR_TEST_BROWSER_CHROMIUM"), os.Getenv("MOLR_TEST_HELPER")
	if agent == "" || chromium == "" || helper == "" {
		t.Skip("native browser assets not configured")
	}
	var revoked atomic.Bool
	r := nativeRunner(t, func(context.Context, core.Execution, platform.Command) error {
		if revoked.Load() {
			return fmt.Errorf("revoked")
		}
		return nil
	})
	r.options.Helper = helper
	slowStarted := make(chan struct{})
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/slow" {
			close(slowStarted)
			time.Sleep(3 * time.Second)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>Molr browser</title><input id="name"><button onclick="document.querySelector('#out').textContent=document.querySelector('#name').value;localStorage.setItem('value',document.querySelector('#name').value);document.cookie='saved=yes; path=/'">Save</button><div id="out"></div>`)
	}))
	defer page.Close()
	e := testExecution()
	c := platform.Command{Script: "exec /molr/helper sandbox-helper browser", Directory: "/work", Managed: true, MaxOutputBytes: 128 << 10, BrowserAssets: true, BrowserProxy: &browser.Proxy{ProxySummary: browser.ProxySummary{Mode: "direct"}}, NetworkAccess: true, ForwardPorts: []int{browser.CommandPort, browser.StreamPort}, Mounts: []platform.Mount{{Source: agent, Destination: "/resources/browser-agent", ReadOnly: true}, {Source: chromium, Destination: "/resources/browser-chromium", ReadOnly: true}}}
	start := func(e core.Execution, c platform.Command) platform.Process {
		t.Helper()
		p, err := r.Start(t.Context(), e, c)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	client := func(e core.Execution) *http.Client {
		return &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return r.DialForward(ctx, e, browser.CommandPort)
		}}}
	}
	call := func(client *http.Client, a browser.Action) browser.Result {
		t.Helper()
		b, _ := json.Marshal(a)
		res, err := client.Post("http://browser.internal/action", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var result browser.Result
		if err = json.NewDecoder(res.Body).Decode(&result); err != nil || !result.OK {
			t.Fatalf("%s: %+v %v", a.Action, result, err)
		}
		return result
	}
	p := start(e, c)
	cl := client(e)
	t.Cleanup(cl.CloseIdleConnections)
	if initial := call(cl, browser.Action{Action: "get_url"}); !bytes.Contains(initial.Data, []byte("about:blank")) {
		t.Fatalf("unexpected startup page: %s", initial.Data)
	}
	navigation := make(chan error, 1)
	go func() {
		payload, _ := json.Marshal(browser.Action{Action: "navigate", URL: page.URL + "/slow"})
		res, err := cl.Post("http://browser.internal/action", "application/json", bytes.NewReader(payload))
		if err == nil {
			defer res.Body.Close()
			var result browser.Result
			err = json.NewDecoder(res.Body).Decode(&result)
			if err == nil && !result.OK {
				err = fmt.Errorf("slow navigation: %s", result.Error)
			}
		}
		navigation <- err
	}()
	select {
	case <-slowStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("slow page not requested")
	}
	// Polling is allowed to return 503, but must not close the browser while
	// its accepted navigation is still running in the daemon.
	if res, err := cl.Get("http://browser.internal/tabs"); err == nil {
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}
	if err := <-navigation; err != nil {
		t.Fatal(err)
	}
	call(cl, browser.Action{Action: "navigate", URL: page.URL})
	call(cl, browser.Action{Action: "fill", Selector: "#name", Text: "中文输入与持久化"})
	call(cl, browser.Action{Action: "click", Selector: "button"})
	snapshot := call(cl, browser.Action{Action: "snapshot"})
	if !bytes.Contains(snapshot.Data, []byte("中文输入与持久化")) {
		t.Fatal(string(snapshot.Data))
	}
	call(cl, browser.Action{Action: "screenshot"})
	work := platform.AssistantWorkspace(r.options.PrivateRoot, "", e.Principal.OwnerID, e.AssistantID)
	shot, err := os.ReadFile(filepath.Join(work, "outputs/browser-screenshot.png"))
	if err != nil || !bytes.HasPrefix(shot, []byte("\x89PNG")) {
		t.Fatalf("screenshot: %v", err)
	}
	call(cl, browser.Action{Action: "new_tab", URL: page.URL})
	tabs := call(cl, browser.Action{Action: "tabs"})
	if !bytes.Contains(tabs.Data, []byte("t2")) {
		t.Fatal(string(tabs.Data))
	}
	call(cl, browser.Action{Action: "select_tab", Tab: "t1"})
	call(cl, browser.Action{Action: "close_tab", Tab: "t2"})
	call(cl, browser.Action{Action: "get_url"})
	call(cl, browser.Action{Action: "viewport", Height: 720})
	d := websocket.Dialer{NetDialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return r.DialForward(ctx, e, browser.StreamPort)
	}}
	ws, _, err := d.DialContext(t.Context(), "ws://browser.internal/", nil)
	if err != nil {
		t.Fatal(err)
	}
	ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, frame, err := ws.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(frame, []byte(`"type":"frame"`)) {
			break
		}
	}
	ws.Close()
	cShell := nativeCommand(fmt.Sprintf("/usr/bin/curl --noproxy '*' --silent --max-time 1 http://127.0.0.1:%d/json/version", browserControlFirst+1))
	cShell.NetworkAccess = true
	other := e
	other.OperationID = "shell-probe"
	shell, err := r.Start(t.Context(), other, cShell)
	if err != nil {
		t.Fatal(err)
	}
	exit, err := shell.Wait(t.Context())
	if err != nil || exit.Code == 0 {
		t.Fatalf("control isolation: %+v %v", exit, err)
	}
	if err = p.Cancel(t.Context()); err != nil {
		t.Fatal(err)
	}
	cl.CloseIdleConnections()
	p = start(e, c)
	cl = client(e)
	defer cl.CloseIdleConnections()
	call(cl, browser.Action{Action: "navigate", URL: page.URL})
	entries, _ := filepath.Glob(filepath.Join(r.options.PrivateRoot, "browser-*", "ipc", "default.sock"))
	if len(entries) != 1 {
		t.Fatalf("private sockets: %v", entries)
	}
	// Use the short alias for Darwin's Unix socket path limit.
	aliases, _ := filepath.Glob("/private/tmp/molr-ipc-*/s")
	socket := ""
	for _, alias := range aliases {
		real, _ := filepath.EvalSymlinks(alias)
		expected, _ := filepath.EvalSymlinks(filepath.Dir(entries[0]))
		if real == expected {
			socket = filepath.Join(alias, "default.sock")
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	state, err := browser.Command(ctx, socket, 0, map[string]any{"action": "evaluate", "script": "({cookie:document.cookie,value:localStorage.getItem('value')})"})
	cancel()
	if err != nil || !state.OK || !bytes.Contains(state.Data, []byte("中文输入与持久化")) || !bytes.Contains(state.Data, []byte("saved=yes")) {
		t.Fatalf("persistence: %+v %v", state, err)
	}
	off := e
	off.AssistantID = "offline"
	off.Principal.SubjectID = "offline"
	off.OperationID = "offline-browser"
	offline := c
	offline.NetworkAccess = false
	poff := start(off, offline)
	offClient := client(off)
	defer offClient.CloseIdleConnections()
	b, _ := json.Marshal(browser.Action{Action: "navigate", URL: page.URL})
	res, err := offClient.Post("http://browser.internal/action", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(raw), "ERR_ACCESS_DENIED") {
		t.Fatalf("offline: %s", raw)
	}
	call(offClient, browser.Action{Action: "navigate", URL: "about:blank"})
	call(offClient, browser.Action{Action: "screenshot"})
	if err = poff.Cancel(t.Context()); err != nil {
		t.Fatal(err)
	}
	revoked.Store(true)
	ctx, cancel = context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	exit, err = p.Wait(ctx)
	if err != nil || exit.Reason != "authorization_changed" {
		t.Fatalf("revocation: %+v %v", exit, err)
	}
	if _, err = r.DialForward(t.Context(), e, browser.StreamPort); err == nil {
		t.Fatal("revoked stream still forwarded")
	}
	revoked.Store(false)
	expiring := c
	expiring.AuthorizationDeadline = time.Now().Add(20 * time.Second)
	p = start(e, expiring)
	deadlineCtx, stopDeadline := context.WithTimeout(t.Context(), 25*time.Second)
	defer stopDeadline()
	exit, err = p.Wait(deadlineCtx)
	if err != nil || exit.Reason != "timeout" || !exit.TimedOut {
		t.Fatalf("browser expiry: %+v %v", exit, err)
	}
	// Restart after forced expiry without manually removing profile locks, then
	// crash only the daemon and require the browser and forwards to be reclaimed.
	p = start(e, c)
	pidFiles, _ := filepath.Glob(filepath.Join(r.options.PrivateRoot, "browser-*", "ipc", "default.pid"))
	if len(pidFiles) != 1 {
		t.Fatal(pidFiles)
	}
	rawPID, err := os.ReadFile(pidFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	daemonPID, err := strconv.Atoi(strings.TrimSpace(string(rawPID)))
	if err != nil || daemonPID < 2 {
		t.Fatal("invalid daemon PID")
	}
	if err = syscall.Kill(daemonPID, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	crashCtx, stopCrash := context.WithTimeout(t.Context(), 10*time.Second)
	defer stopCrash()
	exit, err = p.Wait(crashCtx)
	if err != nil || exit.Reason != "browser_exited" {
		t.Fatalf("daemon crash: %+v %v", exit, err)
	}
}

// TestSeatbeltBrowserCrashWorker is a private subprocess entry for exercising
// the real pipe guardians when the Runtime process cannot run defer/cleanup.
func TestSeatbeltBrowserCrashWorker(t *testing.T) {
	root := os.Getenv("MOLR_BROWSER_CRASH_ROOT")
	if root == "" {
		t.Skip("crash worker only")
	}
	r := New(Options{PrivateRoot: root, Helper: os.Getenv("MOLR_TEST_HELPER"), Authorize: func(context.Context, core.Execution, platform.Command) error { return nil }})
	if err := r.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	c := platform.Command{Script: "exec /molr/helper sandbox-helper browser", Directory: "/work", Managed: true, MaxOutputBytes: 64 << 10, BrowserAssets: true, BrowserProxy: &browser.Proxy{ProxySummary: browser.ProxySummary{Mode: "direct"}}, NetworkAccess: true, ForwardPorts: []int{browser.CommandPort, browser.StreamPort}, Mounts: []platform.Mount{{Source: os.Getenv("MOLR_TEST_BROWSER_AGENT"), Destination: "/resources/browser-agent", ReadOnly: true}, {Source: os.Getenv("MOLR_TEST_BROWSER_CHROMIUM"), Destination: "/resources/browser-chromium", ReadOnly: true}}}
	if _, err := r.Start(t.Context(), testExecution(), c); err != nil {
		t.Fatal(err)
	}
	paths, _ := filepath.Glob(filepath.Join(root, "browser-*", "ipc", "default.stream"))
	if len(paths) != 1 {
		t.Fatal(paths)
	}
	portBytes, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(strings.TrimSpace(string(portBytes)))
	pids := []int{}
	for _, p := range []int{port, port + 1} {
		out, err := exec.Command("/usr/sbin/lsof", "-nP", "-iTCP:"+strconv.Itoa(p), "-sTCP:LISTEN", "-Fp").Output()
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "p") {
				pid, _ := strconv.Atoi(line[1:])
				pids = append(pids, pid)
			}
		}
	}
	ready, _ := json.Marshal(struct {
		Port int
		PIDs []int
	}{port, pids})
	if err = os.WriteFile(filepath.Join(root, "ready.json"), ready, 0600); err != nil {
		t.Fatal(err)
	}
	select {}
}

func TestSeatbeltBrowserParentCrash(t *testing.T) {
	if os.Getenv("MOLR_TEST_BROWSER_AGENT") == "" || os.Getenv("MOLR_TEST_BROWSER_CHROMIUM") == "" || os.Getenv("MOLR_TEST_HELPER") == "" {
		t.Skip("native browser assets not configured")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	log, err := os.Create(filepath.Join(root, "worker.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd := exec.Command(exe, "-test.run=^TestSeatbeltBrowserCrashWorker$", "-test.timeout=90s")
	cmd.Env = append(os.Environ(), "MOLR_BROWSER_CRASH_ROOT="+root)
	cmd.Stdout, cmd.Stderr = log, log
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	var ready struct {
		Port int
		PIDs []int
	}
	deadline := time.Now().Add(40 * time.Second)
	for {
		if raw, err := os.ReadFile(filepath.Join(root, "ready.json")); err == nil && json.Unmarshal(raw, &ready) == nil {
			break
		}
		if time.Now().After(deadline) {
			raw, _ := os.ReadFile(log.Name())
			t.Fatalf("worker not ready: %s", raw)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(ready.PIDs) != 2 {
		t.Fatal(ready)
	}
	guardians, err := exec.Command("/usr/bin/pgrep", "-P", strconv.Itoa(cmd.Process.Pid)).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range strings.Fields(string(guardians)) {
		pid, _ := strconv.Atoi(value)
		ready.PIDs = append(ready.PIDs, pid)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	deadline = time.Now().Add(8 * time.Second)
	for {
		alive := false
		for _, pid := range ready.PIDs {
			if syscall.Kill(pid, 0) == nil {
				alive = true
			}
		}
		listing, _ := exec.Command("/bin/ps", "-axo", "pid=,command=").Output()
		for _, marker := range []string{root, strings.Replace(root, "/private/var/", "/var/", 1)} {
			if strings.Contains(string(listing), marker) {
				alive = true
			}
		}
		if !alive {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("browser survived Runtime SIGKILL: %v", ready.PIDs)
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, port := range []int{ready.Port, ready.Port + 1} {
		l, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			t.Fatalf("control port retained: %v", err)
		}
		l.Close()
	}
	// Runtime cannot remove its socket alias after SIGKILL. Remove only this
	// worker's proven private target. Private session directories are never reused.
	aliases, _ := filepath.Glob("/private/tmp/molr-ipc-*/s")
	for _, alias := range aliases {
		real, _ := filepath.EvalSymlinks(alias)
		if strings.HasPrefix(real, root+"/") {
			os.RemoveAll(filepath.Dir(alias))
		}
	}
}
