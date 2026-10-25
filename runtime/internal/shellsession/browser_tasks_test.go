package shellsession

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/browser"
	"molr.ai/runtime/internal/core"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func browserTaskFixture(t *testing.T) (*Manager, core.Principal, core.Execution, *runningShell) {
	b, p, e := taskFixture(t)
	s := b.shellTasks
	ctx, cancel := context.WithCancel(t.Context())
	r := &runningShell{task: Task{ID: e.OperationID, OwnerID: p.OwnerID, AssistantID: e.AssistantID, Kind: "browser", State: "running", Browser: &BrowserState{Control: "assistant", Generation: 1}}, cancel: cancel, browserCtx: ctx, ready: make(chan struct{}), done: make(chan struct{}), scope: &Control{e: e}}
	close(r.ready)
	r.scope.alive.Store(true)
	s.active[shellKey(e)] = r
	s.browserRoundTrip = func(context.Context, *runningShell, []byte) (json.RawMessage, error) {
		return json.RawMessage(`{"ok":true,"data":{"text":"page"}}`), nil
	}
	return s, p, e, r
}
func TestBrowserTakeoverAllowsReadsRejectsActionsAndOldInputs(t *testing.T) {
	s, p, e, r := browserTaskFixture(t)
	_, err := s.browserRequest(t.Context(), p, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "takeover"}, ClientID: "window-a", Generation: 1})
	need(t, err)
	for _, name := range []string{"snapshot", "screenshot", "tabs", "get_url"} {
		_, err = s.browserRequest(t.Context(), e.Principal, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: name}})
		need(t, err)
	}
	for _, name := range []string{"close", "click", "navigate", "fill", "select_tab"} {
		_, err = s.browserRequest(t.Context(), e.Principal, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: name}})
		if core.ReasonOf(err) != "browser_user_controlled" {
			t.Fatalf("%s: %v", name, err)
		}
	}
	s.mu.Lock()
	generation := r.task.Browser.Generation
	s.mu.Unlock()
	input := tools.BrowserRequest{Action: browser.Action{Action: "press", Key: "Enter"}, ClientID: "window-a", Generation: generation, Sequence: 1}
	_, err = s.browserRequest(t.Context(), p, e.AssistantID, input)
	need(t, err)
	if _, err = s.browserRequest(t.Context(), p, e.AssistantID, input); core.CodeOf(err) != core.Conflict {
		t.Fatal("duplicate input accepted", err)
	}
	input.ClientID = "window-b"
	input.Sequence = 2
	if _, err = s.browserRequest(t.Context(), p, e.AssistantID, input); core.CodeOf(err) != core.Conflict {
		t.Fatal("foreign window input accepted", err)
	}
	_, err = s.browserRequest(t.Context(), p, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "release"}, ClientID: "window-a", Generation: generation})
	need(t, err)
	_, err = s.browserRequest(t.Context(), e.Principal, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "press", Key: "Enter"}})
	if core.ReasonOf(err) != "browser_snapshot_required" {
		t.Fatal("write accepted before fresh snapshot", err)
	}
	_, err = s.browserRequest(t.Context(), e.Principal, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "snapshot"}})
	need(t, err)
	_, err = s.browserRequest(t.Context(), e.Principal, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "navigate", URL: "https://example.com"}})
	need(t, err)
}
func TestBrowserHandoffWaitsForAcceptedOperation(t *testing.T) {
	s, p, e, r := browserTaskFixture(t)
	started, finish := make(chan struct{}), make(chan struct{})
	var count atomic.Int32
	s.browserRoundTrip = func(context.Context, *runningShell, []byte) (json.RawMessage, error) {
		if count.Add(1) == 1 {
			close(started)
			<-finish
		}
		return json.RawMessage(`{"ok":true}`), nil
	}
	commandDone := make(chan error, 1)
	go func() {
		_, err := s.browserRequest(t.Context(), e.Principal, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "click", Selector: "@e1"}})
		commandDone <- err
	}()
	<-started
	handoff := make(chan error, 1)
	go func() {
		_, err := s.browserRequest(t.Context(), p, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "takeover"}, ClientID: "window", Generation: 1})
		handoff <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		switching := r.task.Browser.Control == "switching"
		s.mu.Unlock()
		if switching {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("handoff not started")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-handoff:
		t.Fatalf("handoff completed before operation: %v", err)
	default:
	}
	close(finish)
	need(t, <-commandDone)
	need(t, <-handoff)
	_, err := s.browserRequest(t.Context(), e.Principal, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "press", Key: "Enter"}})
	if core.ReasonOf(err) != "browser_user_controlled" || count.Load() != 2 {
		t.Fatal("model operated after handoff", err, count.Load())
	}
}

func TestBrowserStatusOnlyIdentifiesControllerToItsSession(t *testing.T) {
	s, p, e, r := browserTaskFixture(t)
	s.mu.Lock()
	r.task.Browser.Control = "user"
	r.task.Browser.ControllerSession = p.SessionID
	r.task.Browser.ControllerClient = "original-window"
	s.mu.Unlock()
	raw, err := s.browserStatus(t.Context(), p, e.AssistantID)
	need(t, err)
	var own map[string]any
	need(t, json.Unmarshal(raw, &own))
	if own["ControllerClientID"] != "original-window" {
		t.Fatalf("missing controller identity: %s", raw)
	}
	other := p
	other.SessionID = "different-session"
	raw, err = s.browserStatus(t.Context(), other, e.AssistantID)
	need(t, err)
	var foreign map[string]any
	need(t, json.Unmarshal(raw, &foreign))
	if _, ok := foreign["ControllerClientID"]; ok || foreign["ControllerIsCurrentSession"] != false {
		t.Fatalf("foreign session received controller identity: %s", raw)
	}
}

func TestBrowserStatusRetainsLatestFailureWithoutExposingOutput(t *testing.T) {
	s, p, e, r := browserTaskFixture(t)
	delete(s.active, shellKey(e))
	last := r.task
	last.State = "failed"
	last.StartedAt = time.Now().UTC()
	last.Result = json.RawMessage(`{"Exit":{"Code":125,"Reason":"process_limit"},"Stderr":"sensitive page text"}`)
	need(t, s.save(t.Context(), last))
	read := func() map[string]json.RawMessage {
		t.Helper()
		raw, err := s.browserStatus(t.Context(), p, e.AssistantID)
		need(t, err)
		var v map[string]json.RawMessage
		need(t, json.Unmarshal(raw, &v))
		if strings.Contains(string(raw), "sensitive page text") {
			t.Fatal("raw output leaked")
		}
		return v
	}
	v := read()
	if !strings.Contains(string(v["LastFailure"]), "process_limit") {
		t.Fatal(string(v["LastFailure"]))
	}
	// A newer ordinary shell failure must not hide the browser's result.
	other := last
	other.ID = "shell-other"
	other.Kind = "shell"
	other.StartedAt = last.StartedAt.Add(time.Second)
	need(t, s.save(t.Context(), other))
	if read()["LastFailure"] == nil {
		t.Fatal("shell task hid browser failure")
	}
	// Another assistant's browser cannot replace the scoped failure.
	other.ID = "foreign-browser"
	other.Kind = "browser"
	other.AssistantID = "other-assistant"
	need(t, s.save(t.Context(), other))
	if !strings.Contains(string(read()["LastFailure"]), string(last.ID)) {
		t.Fatal("incorrect assistant scope")
	}
	last.ID = "new-browser"
	last.StartedAt = last.StartedAt.Add(2 * time.Second)
	last.State = "running"
	last.Result = nil
	need(t, s.save(t.Context(), last))
	if read()["LastFailure"] != nil {
		t.Fatal("previous failure survived retry")
	}
	last.State = "stopped"
	last.Result = json.RawMessage(`{"Exit":{"Code":125,"Reason":"canceled"}}`)
	need(t, s.save(t.Context(), last))
	if read()["LastFailure"] != nil {
		t.Fatal("intentional close reported as failure")
	}
	last.State = "interrupted"
	need(t, s.save(t.Context(), last))
	if !strings.Contains(string(read()["LastFailure"]), "interrupted") {
		t.Fatal("restart interruption lost")
	}
}

func TestBrowserStartRejectsDifferentResourceOrMode(t *testing.T) {
	s, _, e, r := browserTaskFixture(t)
	r.task.ComputeID = "compute"
	for _, q := range []tools.BrowserRequest{
		{ComputeID: "other"}, {ComputeID: "compute", FullAccess: true},
	} {
		_, err := s.startBrowser(t.Context(), e, q)
		if core.ReasonOf(err) != "browser_mode_conflict" {
			t.Fatalf("different selection reused browser: %v", err)
		}
	}
	if r.task.FullAccess {
		t.Fatal("existing browser mode changed")
	}
}
