package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/browser"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
)

type browserState struct {
	Control           string
	NeedsSnapshot     bool
	Generation        uint64
	ControllerSession core.ID `json:"-"`
	ControllerClient  string  `json:"-"`
	LastSequence      uint64  `json:"-"`
}

func browserControlled() error {
	return core.WithReason(core.NewError(core.FailedPrecondition, "The user currently controls the browser. You may read snapshots, screenshots, tabs and URL; wait for the user to return control before performing any operation."), "browser_user_controlled")
}
func (s *shellTasks) browserPrograms() (*tools.BrowserLaunch, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	base := filepath.Join(filepath.Dir(exe), "libexec", "browser")
	a, c := s.browserConfig.AgentDirectory, s.browserConfig.ChromiumDirectory
	if a == "" {
		a = filepath.Join(base, "agent")
	}
	if c == "" {
		c = filepath.Join(base, "chromium")
	}
	for _, p := range []string{filepath.Join(a, "agent-browser"), filepath.Join(c, "chrome")} {
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
func (s *shellTasks) activeBrowser(p core.Principal, id, task core.ID) *runningShell {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.active {
		if r.task.OwnerID == p.OwnerID && r.task.AssistantID == id && r.task.Kind == "browser" && r.task.State == "running" && (task == "" || r.task.ID == task) {
			return r
		}
	}
	return nil
}
func (s *shellTasks) browserStatus(p core.Principal, id core.ID) (json.RawMessage, error) {
	_, err := s.browserPrograms()
	if err == nil && (s.router == nil || s.router.shell == nil || !s.router.shell.config.UsesHostTools()) {
		err = core.NewError(core.FailedPrecondition, "browser requires the local host-tools sandbox")
	}
	result := map[string]any{"Available": err == nil}
	if err != nil {
		result["Reason"] = err.Error()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.active {
		if r.task.OwnerID == p.OwnerID && r.task.AssistantID == id && r.task.Kind == "browser" && r.task.State == "running" {
			result["Task"] = r.task
			result["ControllerIsCurrentSession"] = r.task.Browser.ControllerSession == p.SessionID
			break
		}
	}
	return json.Marshal(result)
}
func (s *shellTasks) browserTool(ctx context.Context, e core.Execution, name string, q tools.BrowserRequest) (json.RawMessage, error) {
	if err := (access{s.store}).execution(ctx, e); err != nil {
		return nil, err
	}
	if q.Action.Action == "start" {
		raw, err := s.startBrowser(ctx, e, q)
		if err != nil {
			return nil, err
		}
		if r := s.activeBrowser(e.Principal, e.AssistantID, ""); r != nil {
			if err = s.browserWaitReady(ctx, r); err != nil {
				return nil, err
			}
		}
		return raw, nil
	}
	raw, err := s.browserRequest(ctx, e.Principal, e.AssistantID, q)
	if err == nil && q.Action.Action == "screenshot" && s.browserReadShot != nil {
		return s.browserReadShot(ctx, e, tools.FileReadRequest{ModelID: q.ModelID, FileTarget: tools.FileTarget{Path: "/work/outputs/browser-screenshot.png"}})
	}
	return raw, err
}
func (s *shellTasks) browserManage(ctx context.Context, p core.Principal, id core.ID, q tools.BrowserRequest) (json.RawMessage, error) {
	if err := p.RequireOwner(); err != nil {
		return nil, err
	}
	if err := (access{s.store}).assistant(ctx, p, id); err != nil {
		return nil, err
	}
	if q.Action.Action == "start" {
		e := core.Execution{Principal: core.Principal{OwnerID: p.OwnerID, SubjectID: id, Kind: core.ActorAssistant, ExecutionID: randomOperation()}, AssistantID: id, OperationID: randomOperation()}
		// A user-initiated task still executes under the assistant's effective grants.
		// This short-lived capability is replaced by the task's own managed lease.
		cap := &managedShell{e: e}
		cap.alive.Store(true)
		defer cap.alive.Store(false)
		return s.startBrowser(context.WithValue(ctx, managedShellKey{}, cap), e, q)
	}
	return s.browserRequest(ctx, p, id, q)
}
func (s *shellTasks) startBrowser(ctx context.Context, e core.Execution, q tools.BrowserRequest) (json.RawMessage, error) {
	if r := s.activeBrowser(e.Principal, e.AssistantID, ""); r != nil {
		return s.browserStatus(e.Principal, e.AssistantID)
	}
	if q.ComputeID.Validate() != nil {
		return nil, core.Invalid("compute_id", "select an authorized local compute resource")
	}
	programs, err := s.browserPrograms()
	if err != nil {
		return nil, err
	}
	if s.router.shell == nil || !s.router.shell.config.UsesHostTools() {
		return nil, core.NewError(core.FailedPrecondition, "browser requires the local host-tools sandbox")
	}
	req := tools.ShellRequest{Browser: programs, ComputeID: q.ComputeID, Script: "exec /molr/helper sandbox-helper browser", Background: true, NetworkMode: q.NetworkMode, NetworkIDs: q.NetworkIDs, ForwardPorts: []int{browser.CommandPort, browser.StreamPort}}
	if _, err = s.execute(ctx, e, req); err != nil {
		return nil, err
	}
	return s.browserStatus(e.Principal, e.AssistantID)
}
func (s *shellTasks) browserRequest(ctx context.Context, p core.Principal, id core.ID, q tools.BrowserRequest) (json.RawMessage, error) {
	if q.Action.Action == "status" {
		return s.browserStatus(p, id)
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
	if !q.Action.ReadOnly() {
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
		return nil, err
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
		return nil, core.NewError(core.FailedPrecondition, out.Error)
	}
	if q.Action.Action == "snapshot" && p.Kind == core.ActorAssistant {
		s.mu.Lock()
		r.task.Browser.NeedsSnapshot = false
		s.mu.Unlock()
	}
	return json.Marshal(out)
}
func (s *shellTasks) browserControl(ctx context.Context, p core.Principal, id core.ID, r *runningShell, q tools.BrowserRequest) (json.RawMessage, error) {
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
	return s.browserStatus(p, id)
}
func (s *shellTasks) browserCall(ctx context.Context, r *runningShell, method, path string, body []byte) (json.RawMessage, error) {
	select {
	case <-r.ready:
	case <-r.done:
		return nil, core.NewError(core.Unavailable, "browser task exited")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(call context.Context, _, _ string) (net.Conn, error) {
		return asForwardConn(s.router.dialForward(call, r.scope.e, "local", browser.CommandPort))
	}}
	defer transport.CloseIdleConnections()
	req, _ := http.NewRequestWithContext(ctx, method, "http://browser.internal"+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Transport: transport}).Do(req)
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

// Model start must settle before its next action; UI start observes readiness
// through the reconnecting frame stream while it displays the starting state.
func (s *shellTasks) browserWaitReady(ctx context.Context, r *runningShell) error {
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
