package app

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/browser"
	"molr.ai/runtime/internal/core"
	"sync/atomic"
	"testing"
	"time"
)

func browserTaskFixture(t *testing.T) (*shellTasks, core.Principal, core.Execution, *runningShell) {
	b, p, e := taskFixture(t)
	s := b.shellTasks
	ctx, cancel := context.WithCancel(t.Context())
	r := &runningShell{task: shellTask{ID: e.OperationID, OwnerID: p.OwnerID, AssistantID: e.AssistantID, Kind: "browser", State: "running", Browser: &browserState{Control: "assistant", Generation: 1}}, cancel: cancel, browserCtx: ctx, ready: make(chan struct{}), done: make(chan struct{}), scope: &managedShell{e: e}}
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
	raw, err := s.browserStatus(p, e.AssistantID)
	need(t, err)
	var own map[string]any
	need(t, json.Unmarshal(raw, &own))
	if own["ControllerClientID"] != "original-window" {
		t.Fatalf("missing controller identity: %s", raw)
	}
	other := p
	other.SessionID = "different-session"
	raw, err = s.browserStatus(other, e.AssistantID)
	need(t, err)
	var foreign map[string]any
	need(t, json.Unmarshal(raw, &foreign))
	if _, ok := foreign["ControllerClientID"]; ok || foreign["ControllerIsCurrentSession"] != false {
		t.Fatalf("foreign session received controller identity: %s", raw)
	}
}
