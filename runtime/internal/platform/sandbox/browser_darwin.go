package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"molr.ai/runtime/internal/browser"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/textfiles"
)

var browserPorts = struct {
	sync.Mutex
	used map[int]bool
}{used: map[int]bool{}}

func reserveBrowserPorts() (int, func(), error) {
	browserPorts.Lock()
	defer browserPorts.Unlock()
	for port := browserControlFirst; port < browserControlLast; port += 2 {
		if browserPorts.used[port] {
			continue
		}
		a, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		b, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port+1))
		a.Close()
		if err != nil {
			continue
		}
		b.Close()
		browserPorts.used[port] = true
		return port, func() { browserPorts.Lock(); delete(browserPorts.used, port); browserPorts.Unlock() }, nil
	}
	return 0, nil, unavailable("browser_control_ports_busy")
}

type browserChild struct {
	cmd  *exec.Cmd
	pipe io.WriteCloser
	done chan struct{}
	pid  int
}

func startBrowserChild(ctx context.Context, helper string, cfg browserChildConfig, output io.Writer) (*browserChild, error) {
	cmd := exec.Command(helper, "sandbox-helper", "browser-child")
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin"}
	cmd.SysProcAttr = directProcessAttributes()
	cmd.Stderr = output
	cmd.WaitDelay = time.Second
	pipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		pipe.Close()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		pipe.Close()
		return nil, err
	}
	child := &browserChild{cmd: cmd, pipe: pipe, done: make(chan struct{})}
	go func() { cmd.Wait(); close(child.done) }()
	stop := context.AfterFunc(ctx, func() { pipe.Close() })
	defer stop()
	fail := func(err error) (*browserChild, error) { pipe.Close(); <-child.done; return nil, err }
	if err = json.NewEncoder(pipe).Encode(cfg); err != nil {
		return fail(err)
	}
	if err = json.NewDecoder(io.LimitReader(stdout, 128)).Decode(&child.pid); err != nil {
		return fail(err)
	}
	if child.pid <= 1 {
		return fail(errors.New("invalid browser child identity"))
	}
	return child, nil
}
func (c *browserChild) stop() { c.pipe.Close(); <-c.done }

// Never trust a successfully connected port alone: another Runtime or local
// program might have won the bind race. Both listeners must belong to our child.
func browserOwnsPort(ctx context.Context, pid, port int) bool {
	cmd := exec.CommandContext(ctx, "/usr/sbin/lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fp")
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin"}
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	found := false
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "p") {
			if line != "p"+strconv.Itoa(pid) {
				return false
			}
			found = true
		}
	}
	return found
}

func browserEndpoint(ctx context.Context, port int) (string, error) {
	tr := &http.Transport{Proxy: nil}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 300 * time.Millisecond}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	for {
		req, _ := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		res, err := client.Do(req)
		if err == nil {
			var v struct {
				WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
			}
			err = json.NewDecoder(io.LimitReader(res.Body, 16<<10)).Decode(&v)
			res.Body.Close()
			u, parseErr := url.Parse(v.WebSocketDebuggerURL)
			if err == nil && parseErr == nil && u.Scheme == "ws" && u.Host == fmt.Sprintf("127.0.0.1:%d", port) && strings.HasPrefix(u.Path, "/devtools/browser/") {
				return u.String(), nil
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func closeBrowser(endpoint string, chrome *browserChild) {
	if chrome == nil {
		return
	}
	select {
	case <-chrome.done:
		return
	default:
	}
	if endpoint != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		d := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
		conn, _, err := d.DialContext(ctx, endpoint, nil)
		if err == nil {
			conn.SetWriteDeadline(time.Now().Add(time.Second))
			conn.WriteJSON(map[string]any{"id": 1, "method": "Browser.close"})
			conn.Close()
		}
		cancel()
		select {
		case <-chrome.done:
			return
		case <-time.After(5 * time.Second):
		}
	}
	chrome.stop()
}

func (r *Runner) startBrowser(parent context.Context, e core.Execution, c platform.Command) (_ *process, err error) {
	if r.options.NoWorkspace {
		return nil, core.Invalid("browser", "local assistant workspace required")
	}
	ctx, cancel := context.WithCancel(parent)
	deadline := c.AuthorizationDeadline
	if c.Timeout > 0 {
		d := time.Now().Add(c.Timeout)
		if deadline.IsZero() || d.Before(deadline) {
			deadline = d
		}
	}
	if !deadline.IsZero() {
		cancel()
		ctx, cancel = context.WithDeadline(parent, deadline)
	}
	success := false
	defer func() {
		if !success {
			cancel()
		}
	}()
	work := platform.AssistantWorkspace(r.options.PrivateRoot, r.options.WorkRoot, e.Principal.OwnerID, e.AssistantID)
	if err = platform.EnsureWorkspace(work); err != nil {
		return nil, err
	}
	work, err = filepath.EvalSymlinks(work)
	if err != nil {
		return nil, err
	}
	temp, err := os.MkdirTemp(r.options.PrivateRoot, "browser-")
	if err != nil {
		return nil, err
	}
	temp, err = filepath.EvalSymlinks(temp)
	if err != nil {
		return nil, err
	}
	// A short alias avoids Darwin's 104-byte Unix socket path limit for installs
	// in Application Support. The actual socket remains inside Runtime private data;
	// Seatbelt evaluates the resolved target, never grants /private/tmp as a scope.
	alias, err := os.MkdirTemp("/private/tmp", "molr-ipc-")
	if err != nil {
		os.RemoveAll(temp)
		return nil, err
	}
	cleanup := func() { os.RemoveAll(alias); os.RemoveAll(temp) }
	defer func() {
		if !success {
			cleanup()
		}
	}()
	ipc := filepath.Join(temp, "ipc")
	if err = os.Mkdir(ipc, 0700); err != nil {
		return nil, err
	}
	socketDir := filepath.Join(alias, "s")
	if err = os.Symlink(ipc, socketDir); err != nil {
		return nil, err
	}
	read, err := r.nativeTools()
	if err != nil {
		return nil, err
	}
	write := []string{work, temp}
	global := platform.UserGlobal(r.options.PrivateRoot, e.Principal.OwnerID)
	if _, statErr := os.Stat(global); statErr == nil {
		real, resolveErr := filepath.EvalSymlinks(global)
		if resolveErr != nil {
			return nil, resolveErr
		}
		read = append(read, real)
	}
	var agent, chromium string
	for _, mount := range c.Mounts {
		asset := mount.Destination == "/resources/browser-agent" || mount.Destination == "/resources/browser-chromium"
		var real string
		if asset {
			real, err = filepath.EvalSymlinks(mount.Source)
			if err == nil && real != mount.Source {
				err = core.Invalid("browser", "canonical browser asset directory required")
			}
			for _, protected := range []string{r.options.PrivateRoot, r.options.WorkRoot} {
				if protected != "" && textfiles.Overlaps(real, protected) {
					err = core.Invalid("browser", "browser assets overlap private data")
				}
			}
			if mount.Destination == "/resources/browser-agent" {
				agent = filepath.Join(real, "agent-browser")
			} else {
				chromium = browser.ChromiumExecutable(real)
			}
		} else {
			real, err = r.nativeExternal(mount.Source)
		}
		if err != nil {
			return nil, err
		}
		if mount.ReadOnly {
			read = append(read, real)
		} else {
			write = append(write, real)
		}
	}
	for _, executable := range []string{agent, chromium, r.options.Helper} {
		info, statErr := os.Lstat(executable)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Mode().Perm()&0022 != 0 {
			return nil, unavailable("browser_programs_unavailable")
		}
	}
	port, release, err := reserveBrowserPorts()
	if err != nil {
		return nil, err
	}
	defer func() {
		if !success {
			release()
		}
	}()
	profile, err := seatbeltBrowserProfile(read, write, temp, c.NetworkAccess, []int{port, port + 1})
	if err != nil {
		return nil, err
	}
	profileDir := filepath.Join(work, ".local/state/molr/browser/profile")
	if err = os.MkdirAll(profileDir, 0700); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Join(work, "outputs"), 0700); err != nil {
		return nil, err
	}
	env := []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "HOME=" + work, "CFFIXED_USER_HOME=" + work, "TMPDIR=" + temp, "MAC_CHROMIUM_TMPDIR=" + temp, "LANG=en_US.UTF-8"}
	agentEnv := append(slices.Clone(env), "AGENT_BROWSER_DAEMON=1", "AGENT_BROWSER_SESSION=default", "AGENT_BROWSER_SOCKET_DIR="+socketDir, "AGENT_BROWSER_STREAM_PORT="+strconv.Itoa(port), "AGENT_BROWSER_STREAM_QUALITY=60", "AGENT_BROWSER_STREAM_MAX_WIDTH=1280", fmt.Sprintf("AGENT_BROWSER_STREAM_MAX_HEIGHT=%d", browser.MaxViewportHeight), "TOKIO_WORKER_THREADS=2", "RAYON_NUM_THREADS=2")
	flags := []string{"--headless=new", "--no-sandbox", "--no-first-run", "--no-default-browser-check", "--disable-background-networking", "--disable-component-update", "--disable-default-apps", "--disable-sync", "--metrics-recording-only", "--password-store=basic", "--use-mock-keychain", "--window-size=1280,800", "--hide-scrollbars", "--user-data-dir=" + profileDir, "--remote-debugging-port=" + strconv.Itoa(port+1)}
	proxy := *c.BrowserProxy
	if proxy.Mode == "direct" {
		flags = append(flags, "--no-proxy-server")
	} else {
		flags = append(flags, "--proxy-server="+proxy.Server)
		if len(proxy.Bypass) > 0 {
			flags = append(flags, "--proxy-bypass-list="+strings.Join(proxy.Bypass, ";"))
		}
	}
	flags = append(flags, "about:blank")
	if err = r.options.Authorize(ctx, e, c); err != nil {
		return nil, err
	}
	budget := &outputBudget{left: c.MaxOutputBytes}
	p := &process{stdout: newStream(budget), stderr: newStream(budget), done: make(chan struct{}), cancel: cancel, operation: string(e.Principal.ExecutionID) + "/" + string(e.OperationID)}
	defer func() {
		if !success && err != nil {
			p.stderr.mu.Lock()
			detail := p.stderr.b.String()
			p.stderr.mu.Unlock()
			if len(detail) > 8192 {
				detail = detail[len(detail)-8192:]
			}
			if proxy.Authentication {
				proxyURL, _ := url.Parse(proxy.Server)
				proxyURL.User = url.UserPassword(proxy.Username, proxy.Password)
				detail = browser.RedactProxyError(detail, proxyURL.String())
			}
			if detail = strings.TrimSpace(detail); detail != "" {
				err = fmt.Errorf("%w: %s", err, detail)
			}
		}
	}()
	var output io.Writer = p.stderr
	if c.StderrSink != nil {
		output = io.MultiWriter(c.StderrSink, p.stderr)
	}
	// Launch diagnostics contain no credentials: authentication travels only over
	// the private daemon socket, never arguments, environment or task logs.
	startup, stopStartup := context.WithTimeout(ctx, 30*time.Second)
	defer stopStartup()
	daemon, err := startBrowserChild(startup, r.options.Helper, browserChildConfig{Program: agent, Env: agentEnv, Profile: profile, Work: work}, output)
	if err != nil {
		return nil, err
	}
	var chrome *browserChild
	endpoint := ""
	defer func() {
		if !success {
			if chrome != nil {
				chrome.stop()
			}
			daemon.stop()
		}
	}()
	chrome, err = startBrowserChild(startup, r.options.Helper, browserChildConfig{Program: chromium, Args: flags, Env: env, Profile: profile, Work: work}, output)
	if err != nil {
		return nil, err
	}
	// Stop readiness immediately if either supervised process dies.
	go func() {
		select {
		case <-startup.Done():
		case <-daemon.done:
			stopStartup()
		case <-chrome.done:
			stopStartup()
		}
	}()
	endpoint, err = browserEndpoint(startup, port+1)
	if err != nil {
		return nil, fmt.Errorf("browser readiness: %w", err)
	}
	socket := filepath.Join(socketDir, "default.sock")
	for {
		if b, readErr := os.ReadFile(filepath.Join(ipc, "default.stream")); readErr == nil && strings.TrimSpace(string(b)) == strconv.Itoa(port) {
			break
		}
		select {
		case <-startup.Done():
			return nil, startup.Err()
		case <-time.After(30 * time.Millisecond):
		}
	}
	if !browserOwnsPort(startup, chrome.pid, port+1) || !browserOwnsPort(startup, daemon.pid, port) {
		return nil, unavailable("browser_control_port_conflict")
	}
	launch := map[string]any{"action": "launch", "cdpUrl": endpoint, "downloadPath": filepath.Join(work, "outputs")}
	if proxy.Authentication {
		launch["proxy"] = map[string]any{"server": proxy.Server, "username": proxy.Username, "password": proxy.Password}
	}
	result, err := browser.Command(startup, socket, daemon.pid, launch)
	delete(launch, "proxy")
	if err != nil {
		return nil, err
	}
	if !result.OK {
		return nil, errors.New("browser attach failed")
	}
	result, err = browser.Command(startup, socket, daemon.pid, map[string]any{"action": "viewport", "width": browser.ViewportWidth, "height": 800})
	if err != nil || !result.OK {
		return nil, errors.New("browser viewport failed")
	}
	sandboxData, _ := json.Marshal(map[string]any{"backend": "seatbelt", "isolation": "outer-only", "chromium_sandbox": false})
	dispatch := func(call context.Context, command map[string]any) browser.Result {
		if err := r.options.Authorize(ctx, e, c); err != nil {
			cancel()
			return browser.Result{Error: "browser authorization changed"}
		}
		result, err := browser.Command(call, socket, daemon.pid, command)
		if err != nil {
			// A best-effort tab poll may wait behind an accepted navigation.
			if command["action"] != "tab_list" {
				cancel()
			}
			return browser.Result{Error: "browser transport failed"}
		}
		if proxy.Authentication {
			proxyURL, _ := url.Parse(proxy.Server)
			proxyURL.User = url.UserPassword(proxy.Username, proxy.Password)
			result.Error = browser.RedactProxyError(result.Error, proxyURL.String())
		}
		return result
	}
	handler := browser.NewHandler(browser.Result{OK: true, Data: sandboxData}, func(call context.Context, a browser.Action) browser.Result {
		if a.Action == "close" {
			// The task manager cancels after receiving this acknowledgment.
			// Cancel's cleanup flushes Chrome before reporting task completion.
			return browser.Result{OK: true}
		}
		command, err := browser.NativeCommand(a, work)
		if err != nil {
			return browser.Result{Error: err.Error()}
		}
		return dispatch(call, command)
	}, dispatch)
	listener := newBrowserPipeListener()
	authorized := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if err := r.options.Authorize(ctx, e, c); err != nil {
			cancel()
			http.Error(w, "browser authorization changed", http.StatusForbidden)
			return
		}
		handler.ServeHTTP(w, request)
	})
	server := &http.Server{Handler: authorized, ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 8 << 10}
	go server.Serve(listener)
	p.forward = func(call context.Context, requested int) (net.Conn, error) {
		if ctx.Err() != nil || !slices.Contains(c.ForwardPorts, requested) {
			return nil, core.NewError(core.Forbidden, "port not forwarded by this task")
		}
		if err := r.options.Authorize(ctx, e, c); err != nil {
			return nil, err
		}
		var conn net.Conn
		var err error
		if requested == browser.CommandPort {
			conn, err = listener.dial(call)
		} else {
			conn, err = (&net.Dialer{}).DialContext(call, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
		}
		if err != nil {
			return nil, err
		}
		tracked := &forwardConn{Conn: conn, release: func() {}}
		tracked.setStop(context.AfterFunc(ctx, func() { tracked.Close() }))
		return tracked, nil
	}
	if err = r.options.Authorize(ctx, e, c); err != nil {
		server.Close()
		return nil, err
	}
	success = true
	go func() {
		tick := time.NewTicker(200 * time.Millisecond)
		defer tick.Stop()
		reason := "browser_exited"
		var stopErr error
	loop:
		for {
			select {
			case <-ctx.Done():
				stopErr = ctx.Err()
				break loop
			case <-daemon.done:
				break loop
			case <-chrome.done:
				break loop
			case <-tick.C:
				if stopErr = r.options.Authorize(ctx, e, c); stopErr != nil {
					break loop
				}
			}
		}
		if stopErr != nil {
			reason = "authorization_changed"
			if errors.Is(stopErr, context.Canceled) {
				reason = "canceled"
			}
			if errors.Is(stopErr, context.DeadlineExceeded) {
				reason = "timeout"
				p.exit.TimedOut = true
			}
		}
		cancel()
		server.Close()
		if reason == "authorization_changed" || reason == "timeout" {
			chrome.stop()
		} else {
			closeBrowser(endpoint, chrome)
		}
		daemon.stop()
		cleanup()
		release()
		p.exit.Code, p.exit.Reason = 125, reason
		if reason == "canceled" {
			p.exit.Code = 0
		}
		budget.mu.Lock()
		p.exit.OutputTruncated = budget.truncated
		budget.mu.Unlock()
		p.stdout.finish()
		p.stderr.finish()
		close(p.done)
	}()
	return p, nil
}

// The command API has no host listener; only authenticated Runtime forwarding
// can create one side of this in-memory connection.
type browserPipeListener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func newBrowserPipeListener() *browserPipeListener {
	return &browserPipeListener{conns: make(chan net.Conn), done: make(chan struct{})}
}
func (l *browserPipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *browserPipeListener) Close() error { l.once.Do(func() { close(l.done) }); return nil }
func (l *browserPipeListener) Addr() net.Addr {
	return &net.UnixAddr{Name: "browser.internal", Net: "unix"}
}
func (l *browserPipeListener) dial(ctx context.Context) (net.Conn, error) {
	a, b := net.Pipe()
	select {
	case l.conns <- b:
		return a, nil
	case <-ctx.Done():
		a.Close()
		b.Close()
		return nil, ctx.Err()
	case <-l.done:
		a.Close()
		b.Close()
		return nil, net.ErrClosed
	}
}
