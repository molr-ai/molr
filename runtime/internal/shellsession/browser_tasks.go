package shellsession

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/browser"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/maintenance"
	"molr.ai/runtime/internal/persistence"
)

type BrowserState struct {
	ViewportHeight     int
	Control            string
	NeedsSnapshot      bool
	Generation         uint64
	ControllerSession  core.ID `json:"-"`
	ControllerClient   string  `json:"-"`
	Proxy              browser.ProxySummary
	LastNetworkFailure *browserNetworkFailure `json:",omitempty"`
	LastSequence       uint64                 `json:"-"`
}

func browserControlled() error {
	return core.WithReason(core.NewError(core.FailedPrecondition, "The user currently controls the browser. You may read snapshots, screenshots, tabs and URL; wait for the user to return control before performing any operation."), "browser_user_controlled")
}
func (s *Manager) browserPrograms() (*tools.BrowserLaunch, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	base := filepath.Join(filepath.Dir(exe), "libexec", "browser")
	a, c := s.browserConfig.AgentDirectory, s.browserConfig.ChromiumDirectory
	if a == "" && c == "" && s.maintenance != nil {
		agent, chrome, err := s.maintenance.Programs()
		if err == nil {
			return &tools.BrowserLaunch{AgentDirectory: agent, ChromiumDirectory: chrome}, nil
		}
		var unavailable *maintenance.ComponentError
		if !errors.As(err, &unavailable) || unavailable.Code != "maintenance_not_installed" {
			return nil, core.WithReason(core.NewError(core.FailedPrecondition, "Install the compatible browser components before starting."), "browser_programs_unavailable")
		}
	}
	if a == "" {
		a = filepath.Join(base, "agent")
	}
	if c == "" {
		c = filepath.Join(base, "chromium")
	}
	for _, p := range []string{filepath.Join(a, "agent-browser"), browser.ChromiumExecutable(c)} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return nil, core.Invalid("browser", "program directories must be absolute")
		}
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil || resolved != p {
			return nil, core.WithReason(core.NewError(core.FailedPrecondition, "Browser programs are not installed. Configure browser.agent_directory and browser.chromium_directory."), "browser_programs_unavailable")
		}
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 || info.Mode()&0022 != 0 {
			return nil, core.NewError(core.FailedPrecondition, "browser executable must be a regular non-writable program")
		}
	}
	return &tools.BrowserLaunch{AgentDirectory: a, ChromiumDirectory: c}, nil
}
func (s *Manager) activeBrowser(p core.Principal, id, task core.ID) *runningShell {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.active {
		if r.task.OwnerID == p.OwnerID && r.task.AssistantID == id && r.task.Kind == "browser" && r.task.State == "running" && (task == "" || r.task.ID == task) {
			return r
		}
	}
	return nil
}
func (s *Manager) browserStatus(ctx context.Context, p core.Principal, id core.ID) (json.RawMessage, error) {
	_, err := s.browserPrograms()
	sandboxAvailable := s.host.BrowserSandboxAvailable()
	fullAvailable := (runtime.GOOS == "linux" || runtime.GOOS == "darwin") && os.Geteuid() != 0
	proxy, loadErr := s.loadBrowserProxy(ctx)
	if loadErr != nil {
		return nil, loadErr
	}
	result := map[string]any{"Proxy": proxy.ProxySummary, "ProxyAppliesTo": "next_start", "RestartRequired": false, "Available": err == nil, "SandboxAvailable": sandboxAvailable, "FullAccessAvailable": fullAvailable, "ComponentsEnabled": s.maintenance != nil && s.maintenance.Enabled()}
	s.mu.Lock()
	if pending := s.preparing[string(p.OwnerID)+"/"+string(id)]; pending != nil {
		copy := *pending
		result["Preparation"] = &copy
	}
	s.mu.Unlock()
	if err != nil {
		result["Reason"] = err.Error()
	}
	r := s.activeBrowser(p, id, "")
	if r != nil {
		s.refreshBrowserTabCount(r)
		s.mu.Lock()
		defer s.mu.Unlock()
		if r.task.State == "running" {
			result["Task"] = r.task
			result["Proxy"] = r.task.Browser.Proxy
			result["ProxyAppliesTo"] = "current_session"
			result["RestartRequired"] = r.task.Browser.Proxy.Version != proxy.Version
			if r.task.Browser.LastNetworkFailure != nil {
				result["LastNetworkFailure"] = r.task.Browser.LastNetworkFailure
			}
			current := p.SessionID != "" && r.task.Browser.ControllerSession == p.SessionID
			result["ControllerIsCurrentSession"] = current
			if current {
				result["ControllerClientID"] = r.task.Browser.ControllerClient
			}
			if r.browserTabCount != nil {
				result["TabCount"] = *r.browserTabCount
			}
		}
	}
	if r == nil {
		rows, queryErr := persistence.Select[Task](ctx, s.store, shellTasksBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID), persistence.Eq("AssistantID", id), persistence.Eq("Kind", "browser")}, OrderBy: "StartedAtKey", Descending: true, Limit: 1})
		if queryErr != nil {
			return nil, queryErr
		}
		if len(rows) == 1 {
			last := rows[0]
			if last.Browser != nil && last.Browser.LastNetworkFailure != nil {
				result["LastNetworkFailure"] = last.Browser.LastNetworkFailure
			}
			var out struct{ Exit struct{ Reason string } }
			_ = json.Unmarshal(last.Result, &out)
			if last.State == "failed" || last.State == "interrupted" || last.State == "stopped" && out.Exit.Reason != "canceled" {
				reason := "browser_exited"
				switch out.Exit.Reason {
				case "process_limit", "memory_limit", "timeout", "authorization_changed", "disk_space_low", "network_gateway_failed":
					reason = out.Exit.Reason
				}
				if last.State == "interrupted" {
					reason = "interrupted"
				}
				failure := map[string]any{"TaskID": last.ID, "Reason": reason}
				if last.Browser != nil {
					failure["Proxy"] = last.Browser.Proxy
				}
				result["LastFailure"] = failure
			}
		}
	}
	return json.Marshal(result)
}
func (s *Manager) BrowserTool(ctx context.Context, e core.Execution, name string, q tools.BrowserRequest) (json.RawMessage, error) {
	if err := s.host.CheckExecution(ctx, e); err != nil {
		return nil, err
	}
	if q.Action.Action == "start" {
		raw, err := s.startBrowser(ctx, e, q)
		if err != nil {
			return nil, err
		}
		if r := s.activeBrowser(e.Principal, e.AssistantID, ""); r != nil {
			if err = s.browserWaitReady(ctx, r); err != nil {
				s.mu.Lock()
				proxy := r.task.Browser.Proxy
				s.mu.Unlock()
				summary, _ := json.Marshal(proxy)
				return nil, &core.Error{Code: core.CodeOf(err), Reason: "browser_start_failed", Message: "Browser startup did not complete. Inspect browser_session status and task output. Launch proxy settings: " + string(summary)}
			}
		} else {
			return nil, core.WithReason(core.NewError(core.FailedPrecondition, "browser startup failed; inspect task output"), "browser_start_failed")
		}
		return raw, nil
	}
	raw, err := s.browserRequest(ctx, e.Principal, e.AssistantID, q)
	if err == nil && q.Action.Action == "screenshot" && s.browserReadShot != nil {
		return s.browserReadShot(ctx, e, tools.FileReadRequest{ModelID: q.ModelID, FileTarget: tools.FileTarget{Path: "/work/outputs/browser-screenshot.png"}})
	}
	return raw, err
}
func (s *Manager) BrowserManage(ctx context.Context, p core.Principal, id core.ID, q tools.BrowserRequest) (json.RawMessage, error) {
	if err := p.RequireOwner(); err != nil {
		return nil, err
	}
	if err := s.host.CheckAssistant(ctx, p, id); err != nil {
		return nil, err
	}
	if q.Action.Action == "install_start" {
		return s.prepareBrowser(ctx, p, id, q)
	}
	if q.Action.Action == "start" {
		if s.maintenance != nil && s.maintenance.Enabled() {
			if _, err := s.browserPrograms(); err != nil {
				return s.prepareBrowser(ctx, p, id, q)
			}
		}
		e := core.Execution{Principal: core.Principal{OwnerID: p.OwnerID, SubjectID: id, Kind: core.ActorAssistant, ExecutionID: randomOperation()}, AssistantID: id, OperationID: randomOperation()}
		// A user-initiated task still executes under the assistant's effective grants.
		// This short-lived capability is replaced by the task's own managed lease.
		cap := &Control{e: e}
		cap.alive.Store(true)
		defer cap.alive.Store(false)
		return s.startBrowser(context.WithValue(ctx, managedShellKey{}, cap), e, q)
	}
	return s.browserRequest(ctx, p, id, q)
}
func (s *Manager) startBrowser(ctx context.Context, e core.Execution, q tools.BrowserRequest) (json.RawMessage, error) {
	if r := s.activeBrowser(e.Principal, e.AssistantID, ""); r != nil {
		s.mu.Lock()
		matches := r.task.ComputeID == q.ComputeID && r.task.FullAccess == q.FullAccess
		s.mu.Unlock()
		if !matches {
			return nil, core.WithReason(core.NewError(core.Conflict, "close the current browser before changing compute resource or execution mode"), "browser_mode_conflict")
		}
		if err := s.host.AuthorizeSession(context.WithValue(ctx, managedShellKey{}, r.scope), r.scope.e); err != nil {
			return nil, err
		}
		return s.browserStatus(ctx, e.Principal, e.AssistantID)
	}
	if q.ComputeID.Validate() != nil {
		return nil, core.Invalid("compute_id", "select an authorized local compute resource")
	}
	programs, err := s.browserPrograms()
	if err != nil {
		if s.maintenance != nil && s.maintenance.Enabled() {
			_, startErr := s.maintenance.Start(ctx, "install", "browser", browserInstallKey(), "")
			if startErr != nil {
				return nil, startErr
			}
			return nil, core.WithReason(core.NewError(core.FailedPrecondition, "Browser components are being installed. Read browser status and retry after installation."), "component_installing")
		}
		return nil, err
	}
	if !s.host.BrowserSupported(q.FullAccess) {
		return nil, core.NewError(core.FailedPrecondition, "browser requires the local host-tools sandbox")
	}
	programs.Proxy, err = s.loadBrowserProxy(ctx)
	if err != nil {
		return nil, err
	}
	req := tools.ShellRequest{FullAccess: q.FullAccess, Browser: programs, ComputeID: q.ComputeID, Script: "exec /molr/helper sandbox-helper browser", Background: true, NetworkMode: q.NetworkMode, NetworkIDs: q.NetworkIDs, ForwardPorts: []int{browser.CommandPort, browser.StreamPort}}
	if _, err = s.Execute(ctx, e, req); err != nil {
		return nil, err
	}
	return s.browserStatus(ctx, e.Principal, e.AssistantID)
}
func (s *Manager) browserRequest(ctx context.Context, p core.Principal, id core.ID, q tools.BrowserRequest) (json.RawMessage, error) {
	if q.Action.Action == "input_batch" {
		return s.browserInputBatch(ctx, p, id, q)
	}
	if q.Action.Action == "status" {
		return s.browserStatus(ctx, p, id)
	}
	r := s.activeBrowser(p, id, q.TaskID)
	if r == nil {
		return nil, core.NewError(core.NotFound, "no active browser task")
	}
	if q.Action.Action == "takeover" || q.Action.Action == "release" {
		return s.browserControl(ctx, p, id, r, q)
	}
	// Capture the generation before waiting; an action queued before handoff must
	// not run against a new controller's page after acquiring the gate.
	s.mu.Lock()
	generation := r.task.Browser.Generation
	s.mu.Unlock()
	r.browserGate.Lock()
	defer r.browserGate.Unlock()
	s.mu.Lock()
	state := *r.task.Browser
	uncertain := r.browserUncertain
	s.mu.Unlock()
	if uncertain {
		return nil, core.NewError(core.FailedPrecondition, "browser action outcome unknown; close and restart the browser")
	}
	if q.Action.Action == "viewport" {
		if err := p.RequireOwner(); err != nil {
			return nil, err
		}
		if q.TaskID == "" || q.ClientID == "" || q.Generation != state.Generation || generation != state.Generation || state.Control == "switching" ||
			state.Control == "user" && (state.ControllerSession != p.SessionID || state.ControllerClient != q.ClientID) {
			return nil, core.NewError(core.Conflict, "browser viewport controller changed")
		}
		if _, err := q.Action.Args(); err != nil {
			return nil, core.Invalid("viewport", err.Error())
		}
		height := state.ViewportHeight
		if height == 0 {
			height = 800
		}
		if height == q.Height {
			return json.Marshal(browser.Result{OK: true})
		}
	} else if !q.Action.ReadOnly() {
		if p.Kind == core.ActorAssistant && state.Control != "assistant" {
			return nil, browserControlled()
		}
		if p.Kind == core.ActorAssistant && state.NeedsSnapshot {
			return nil, core.WithReason(core.NewError(core.FailedPrecondition, "Read a fresh browser snapshot after the user returns control before operating."), "browser_snapshot_required")
		}
		if generation != state.Generation {
			return nil, core.NewError(core.Conflict, "browser control changed; refresh the snapshot")
		}
		if p.Kind != core.ActorAssistant && q.Action.Action != "close" {
			if state.Control != "user" || state.ControllerSession != p.SessionID || state.ControllerClient != q.ClientID || q.Generation != state.Generation {
				return nil, core.NewError(core.Conflict, "take control in this window before operating")
			}
			if q.Sequence == 0 || q.Sequence <= state.LastSequence {
				return nil, core.NewError(core.Conflict, "stale browser input")
			}
			s.mu.Lock()
			r.task.Browser.LastSequence = q.Sequence
			s.mu.Unlock()
		}
	}
	if q.Action.Action == "close" {
		call, cancel := context.WithTimeout(r.browserCtx, 18*time.Second)
		raw, err := s.browserCall(call, r, "POST", "/action", []byte(`{"action":"close"}`))
		var result browser.Result
		graceful := err == nil && json.Unmarshal(raw, &result) == nil && result.OK
		cancel()
		r.cancel()
		return json.Marshal(map[string]bool{"Stopping": true, "Graceful": graceful})
	}
	if _, err := q.Action.Args(); err != nil {
		return nil, core.Invalid("action", err.Error())
	}
	call, cancel := context.WithTimeout(r.browserCtx, 20*time.Second)
	defer cancel()
	raw, err := json.Marshal(q.Action)
	if err != nil {
		return nil, err
	}
	startedAt := time.Now().UTC()
	var result json.RawMessage
	if s.browserRoundTrip != nil {
		result, err = s.browserRoundTrip(call, r, raw)
	} else {
		result, err = s.browserCall(call, r, "POST", "/action", raw)
	}
	if err != nil {
		// A lost response can conceal an accepted operation. Stop the entire task,
		// rather than report a successful takeover while the daemon may still act.
		if !q.Action.ReadOnly() {
			s.mu.Lock()
			r.browserUncertain = true
			s.mu.Unlock()
			r.cancel()
		}
		return nil, s.browserNetworkError(ctx, r, q.Action, startedAt, err.Error())
	}
	var out browser.Result
	if json.Unmarshal(result, &out) != nil {
		if !q.Action.ReadOnly() {
			s.mu.Lock()
			r.browserUncertain = true
			s.mu.Unlock()
			r.cancel()
		}
		return nil, core.NewError(core.Unavailable, "invalid browser response")
	}
	if !out.OK {
		return nil, s.browserNetworkError(ctx, r, q.Action, startedAt, out.Error)
	}
	if browserNavigation(q.Action) {
		s.clearBrowserNetworkFailure(ctx, r)
	}
	if q.Action.Action == "viewport" {
		s.mu.Lock()
		r.task.Browser.ViewportHeight = q.Height
		r.task.Browser.NeedsSnapshot = true
		s.mu.Unlock()
	}
	if q.Action.Action == "snapshot" && p.Kind == core.ActorAssistant {
		s.mu.Lock()
		r.task.Browser.NeedsSnapshot = false
		s.mu.Unlock()
	}
	return json.Marshal(out)
}

// Each input rechecks ownership under the normal gate. A handoff can invalidate
// the remaining batch while waiting for the currently accepted input to settle.
// Completed counts only confirmed successes; a failed/unknown input is never retried.
func (s *Manager) browserInputBatch(ctx context.Context, p core.Principal, id core.ID, q tools.BrowserRequest) (json.RawMessage, error) {
	if err := p.RequireOwner(); err != nil {
		return nil, err
	}
	if err := q.ValidateBatch(); err != nil {
		return nil, err
	}
	result := struct {
		Completed int         `json:"completed"`
		Code      core.Code   `json:"code,omitempty"`
		Reason    core.Reason `json:"reason,omitempty"`
	}{}
	started := time.Now()
	for i, a := range q.Actions {
		if i > 0 && time.Since(started) >= 250*time.Millisecond {
			break
		}
		if err := ctx.Err(); err != nil {
			result.Code = core.Canceled
			break
		}
		next := q
		next.Action, next.Actions, next.Sequence = a, nil, q.Sequence+uint64(i)
		if _, err := s.browserRequest(ctx, p, id, next); err != nil {
			result.Code, result.Reason = core.CodeOf(err), core.ReasonOf(err)
			break
		}
		result.Completed++
	}
	return json.Marshal(result)
}
func (s *Manager) browserControl(ctx context.Context, p core.Principal, id core.ID, r *runningShell, q tools.BrowserRequest) (json.RawMessage, error) {
	if err := p.RequireOwner(); err != nil {
		return nil, err
	}
	if q.ClientID == "" {
		return nil, core.Invalid("client_id", "browser window identifier required")
	}
	s.mu.Lock()
	state := r.task.Browser
	if q.Generation != state.Generation {
		s.mu.Unlock()
		return nil, core.NewError(core.Conflict, "control changed; refresh browser status")
	}
	if state.Control == "switching" {
		s.mu.Unlock()
		return nil, core.NewError(core.Conflict, "browser handoff already in progress")
	}
	if q.Action.Action == "release" && (state.Control != "user" || state.ControllerSession != p.SessionID || state.ControllerClient != q.ClientID) {
		s.mu.Unlock()
		return nil, denied()
	}
	state.Control = "switching"
	state.Generation++
	if err := s.save(ctx, r.task); err != nil {
		r.browserUncertain = true
		s.mu.Unlock()
		r.cancel()
		return nil, err
	}
	s.mu.Unlock()
	r.browserGate.Lock()
	defer r.browserGate.Unlock()
	s.mu.Lock()
	if r.task.State != "running" || r.browserUncertain {
		s.mu.Unlock()
		return nil, core.NewError(core.FailedPrecondition, "browser stopped during handoff")
	}
	s.mu.Unlock()
	call, cancel := context.WithTimeout(r.browserCtx, 20*time.Second)
	var reset json.RawMessage
	var resetErr error
	if s.browserRoundTrip == nil {
		resetErr = s.browserWaitReady(call, r)
	}
	if resetErr != nil {
		cancel()
		s.mu.Lock()
		r.browserUncertain = true
		s.mu.Unlock()
		r.cancel()
		return nil, resetErr
	}
	if s.browserRoundTrip != nil {
		reset, resetErr = s.browserRoundTrip(call, r, []byte(`{"action":"reset_input"}`))
	} else {
		reset, resetErr = s.browserCall(call, r, "POST", "/reset-input", nil)
	}
	cancel()
	var resetResult browser.Result
	if resetErr != nil || json.Unmarshal(reset, &resetResult) != nil || !resetResult.OK {
		s.mu.Lock()
		r.browserUncertain = true
		s.mu.Unlock()
		r.cancel()
		return nil, core.NewError(core.Unavailable, "browser input reset failed; task stopped")
	}
	s.mu.Lock()
	state = r.task.Browser
	state.Generation++
	state.LastSequence = 0
	state.Control = "assistant"
	state.NeedsSnapshot = true
	state.ControllerSession = ""
	state.ControllerClient = ""
	if q.Action.Action == "takeover" {
		state.Control = "user"
		state.ControllerSession = p.SessionID
		state.ControllerClient = q.ClientID
	}
	err := s.store.Write(context.Background(), func(ctx context.Context, tx persistence.Tx) error {
		if err := s.save(ctx, r.task); err != nil {
			return err
		}
		if q.Action.Action == "release" {
			details, _ := json.Marshal(map[string]any{"BrowserTaskID": r.task.ID, "Notice": "The user returned browser control. Read a fresh browser snapshot before continuing; old element references and queued operations must not be reused."})
			return event.NewWriter(s.store).AppendAssistant(ctx, event.AssistantEvent{ID: randomOperation(), OwnerID: p.OwnerID, AssistantID: id, Kind: event.BrowserControlReturned, DeliveryMode: event.WhenIdle, Details: details, CreatedAt: time.Now().UTC()})
		}
		return nil
	})
	if err != nil {
		r.browserUncertain = true
		r.cancel()
	}
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s.browserStatus(ctx, p, id)
}
func (s *Manager) browserCall(ctx context.Context, r *runningShell, method, path string, body []byte) (json.RawMessage, error) {
	select {
	case <-r.ready:
	case <-r.done:
		return nil, core.NewError(core.Unavailable, "browser task exited")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// Reuse a bounded pool only within this task. The managed execution still
	// enforces its grants and closes forwarded sockets on revocation or exit.
	r.browserHTTPOnce.Do(func() {
		r.browserHTTP = newBrowserHTTPClient(r.browserCtx, func(call context.Context, _, _ string) (net.Conn, error) {
			return NetConn(s.host.DialForward(call, r.scope.e, "local", browser.CommandPort))
		})
	})
	call, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.browserCtx, cancel)
	defer stop()
	defer cancel()
	if err := r.browserCtx.Err(); err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(call, method, "http://browser.internal"+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.browserHTTP.Do(req)
	if err != nil {
		return nil, core.NewError(core.Unavailable, "browser is starting or disconnected")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, core.NewError(core.FailedPrecondition, "browser rejected request")
	}
	return raw, nil
}

// POST actions have no idempotency header, so Transport must not replay an
// accepted input when a reused connection loses its response.
func newBrowserHTTPClient(ctx context.Context, dial func(context.Context, string, string) (net.Conn, error)) *http.Client {
	transport := &http.Transport{
		Proxy: nil, DialContext: dial,
		MaxConnsPerHost: 2, MaxIdleConns: 2, MaxIdleConnsPerHost: 2,
		IdleConnTimeout: 30 * time.Second,
	}
	context.AfterFunc(ctx, transport.CloseIdleConnections)
	return &http.Client{Transport: transport}
}

// Model start must settle before its next action; UI start observes readiness
// through the reconnecting frame stream while it displays the starting state.
func (s *Manager) browserWaitReady(ctx context.Context, r *runningShell) error {
	call, cancel := context.WithTimeout(r.browserCtx, 25*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	for {
		probe, done := context.WithTimeout(call, time.Second)
		_, err := s.browserCall(probe, r, "GET", "/ready", nil)
		done()
		if err == nil {
			return nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-call.Done():
			timer.Stop()
			return core.NewError(core.Unavailable, "Browser startup is still pending; inspect status before retrying.")
		case <-r.done:
			timer.Stop()
			return core.NewError(core.Unavailable, "Browser failed to start; inspect task output and installed programs.")
		case <-timer.C:
		}
	}
}

// Metadata remains observable when no panel is streaming. Cache per task so
// multiple viewers do not multiply daemon requests; failures retain known data.
func (s *Manager) refreshBrowserTabCount(r *runningShell) {
	s.mu.Lock()
	if r.browserTabsLoading || time.Since(r.browserTabsAt) < 2*time.Second || s.browserRoundTrip != nil {
		s.mu.Unlock()
		return
	}
	r.browserTabsLoading = true
	r.browserTabsAt = time.Now()
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.browserCtx, 1200*time.Millisecond)
	defer cancel()
	raw, err := s.browserCall(ctx, r, "GET", "/tabs", nil)
	var out struct {
		OK   bool `json:"ok"`
		Data struct {
			Tabs []json.RawMessage `json:"tabs"`
		} `json:"data"`
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r.browserTabsLoading = false
	if err == nil && json.Unmarshal(raw, &out) == nil && out.OK {
		n := len(out.Data.Tabs)
		r.browserTabCount = &n
	}
}

// Browser preparation is instance-local and is never replayed after Runtime restart.
type browserPreparation struct {
	TaskID    string `json:"task_id"`
	State     string `json:"state"`
	ErrorCode string `json:"error_code,omitempty"`
}

func (s *Manager) prepareBrowser(ctx context.Context, p core.Principal, id core.ID, q tools.BrowserRequest) (json.RawMessage, error) {
	if s.maintenance == nil {
		return nil, core.NewError(core.FailedPrecondition, "component installation unavailable")
	}
	if q.ComputeID.Validate() != nil {
		return nil, core.Invalid("compute_id", "select a compute resource")
	}
	job, err := s.maintenance.Start(ctx, "install", "browser", browserInstallKey(), "")
	if err != nil {
		return nil, err
	}
	key := string(p.OwnerID) + "/" + string(id)
	s.mu.Lock()
	if s.preparing == nil {
		s.preparing = map[string]*browserPreparation{}
	}
	if old := s.preparing[key]; old != nil && old.State == "installing" {
		s.mu.Unlock()
		return s.browserStatus(ctx, p, id)
	}
	pending := &browserPreparation{TaskID: job.ID, State: "installing"}
	s.preparing[key] = pending
	s.mu.Unlock()
	go func() {
		wait, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		fail := func(code string) { s.mu.Lock(); pending.State = "failed"; pending.ErrorCode = code; s.mu.Unlock() }
		for {
			s.mu.Lock()
			stopped := s.stopped
			s.mu.Unlock()
			if stopped {
				return
			}
			status, err := s.maintenance.Task(wait, job.ID)
			if err != nil {
				fail("maintenance_failed")
				return
			}
			if !status.Active() {
				if status.State != "succeeded" {
					fail(status.ErrorCode)
					return
				}
				break
			}
			select {
			case <-wait.Done():
				fail("maintenance_interrupted")
				return
			case <-ticker.C:
			}
		}
		if s.identity != nil {
			watch, err := s.identity.WatchPrincipal(p)
			if err != nil {
				fail("unauthenticated")
				return
			}
			defer watch.Close()
			if _, err := watch.Check(wait); err != nil {
				fail("unauthenticated")
				return
			}
		}
		q.Action.Action = "start"
		if _, err := s.BrowserManage(wait, p, id, q); err != nil {
			fail("browser_start_failed")
			return
		}
		s.mu.Lock()
		pending.State = "ready"
		s.mu.Unlock()
	}()
	return s.browserStatus(ctx, p, id)
}

func browserInstallKey() string {
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(key[:])
}
