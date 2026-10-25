package shellsession

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/browser"
	"molr.ai/runtime/internal/core"
)

func TestBrowserInputBatchOrderFailureAndHandoff(t *testing.T) {
	for _, mode := range []string{"success", "failure", "handoff", "slow"} {
		t.Run(mode, func(t *testing.T) {
			s, p, e, r := browserTaskFixture(t)
			r.task.Browser.Control, r.task.Browser.ControllerSession, r.task.Browser.ControllerClient = "user", p.SessionID, "window"
			var seen []string
			s.browserRoundTrip = func(_ context.Context, _ *runningShell, raw []byte) (json.RawMessage, error) {
				var a browser.Action
				need(t, json.Unmarshal(raw, &a))
				seen = append(seen, a.Action)
				if len(seen) == 1 {
					switch mode {
					case "failure":
						return json.RawMessage(`{"ok":false,"error":"rejected"}`), nil
					case "handoff":
						s.mu.Lock()
						r.task.Browser.Generation++
						s.mu.Unlock()
					case "slow":
						time.Sleep(260 * time.Millisecond)
					}
				}
				return json.RawMessage(`{"ok":true}`), nil
			}
			q := tools.BrowserRequest{Action: browser.Action{Action: "input_batch"}, ClientID: "window", Generation: 1, Sequence: 1, Actions: []browser.Action{{Action: "insert_text", Text: "a"}, {Action: "press", Key: "Tab"}, {Action: "insert_text", Text: "b"}}}
			raw, err := s.browserRequest(t.Context(), p, e.AssistantID, q)
			need(t, err)
			var out struct {
				Completed int
				Code      core.Code
			}
			need(t, json.Unmarshal(raw, &out))
			switch mode {
			case "success":
				if out.Completed != 3 || out.Code != "" || len(seen) != 3 {
					t.Fatal(string(raw), seen)
				}
				// An accepted batch cannot be replayed.
				raw, err = s.browserRequest(t.Context(), p, e.AssistantID, q)
				need(t, err)
				need(t, json.Unmarshal(raw, &out))
				if out.Code != core.Conflict || len(seen) != 3 {
					t.Fatal(string(raw), seen)
				}
			case "failure":
				if out.Completed != 0 || out.Code != core.FailedPrecondition || len(seen) != 1 {
					t.Fatal(string(raw), seen)
				}
			case "handoff":
				if out.Completed != 1 || out.Code != core.Conflict || len(seen) != 1 {
					t.Fatal(string(raw), seen)
				}
			case "slow":
				if out.Completed != 1 || out.Code != "" || len(seen) != 1 {
					t.Fatal(string(raw), seen)
				}
			}
		})
	}
}

func TestBrowserInputBatchRejectsInvalidSuffixBeforeExecution(t *testing.T) {
	s, p, e, r := browserTaskFixture(t)
	r.task.Browser.Control, r.task.Browser.ControllerSession, r.task.Browser.ControllerClient = "user", p.SessionID, "window"
	calls := 0
	s.browserRoundTrip = func(context.Context, *runningShell, []byte) (json.RawMessage, error) {
		calls++
		return json.RawMessage(`{"ok":true}`), nil
	}
	q := tools.BrowserRequest{Action: browser.Action{Action: "input_batch"}, ClientID: "window", Generation: 1, Sequence: 1, Actions: []browser.Action{{Action: "insert_text", Text: "a"}, {Action: "navigate", URL: "https://example.com"}}}
	if _, err := s.browserRequest(t.Context(), p, e.AssistantID, q); core.CodeOf(err) != core.InvalidArgument || calls != 0 {
		t.Fatal(err, calls)
	}
	q.Actions = q.Actions[:1]
	if _, err := s.browserRequest(t.Context(), e.Principal, e.AssistantID, q); err == nil || calls != 0 {
		t.Fatal("model accepted batch", err, calls)
	}
}

func TestBrowserViewportOwnershipAndSnapshot(t *testing.T) {
	s, p, e, r := browserTaskFixture(t)
	calls := 0
	s.browserRoundTrip = func(_ context.Context, _ *runningShell, raw []byte) (json.RawMessage, error) {
		calls++
		return json.RawMessage(`{"ok":true}`), nil
	}
	q := tools.BrowserRequest{Action: browser.Action{Action: "viewport", Height: 2400}, TaskID: r.task.ID, ClientID: "window", Generation: 1}
	_, err := s.browserRequest(t.Context(), p, e.AssistantID, q)
	need(t, err)
	if calls != 1 || r.task.Browser.ViewportHeight != 2400 || !r.task.Browser.NeedsSnapshot {
		t.Fatal(calls, r.task.Browser)
	}
	_, err = s.browserRequest(t.Context(), p, e.AssistantID, q)
	need(t, err)
	if calls != 1 {
		t.Fatal("duplicate resize", calls)
	}
	if _, err = s.browserRequest(t.Context(), e.Principal, e.AssistantID, q); err == nil {
		t.Fatal("assistant resized viewport")
	}
	r.task.Browser.Control, r.task.Browser.ControllerSession, r.task.Browser.ControllerClient = "user", p.SessionID, "window"
	q.Height = 1600
	q.ClientID = "observer"
	if _, err = s.browserRequest(t.Context(), p, e.AssistantID, q); core.CodeOf(err) != core.Conflict {
		t.Fatal(err)
	}
	q.ClientID = "window"
	q.Generation = 0
	if _, err = s.browserRequest(t.Context(), p, e.AssistantID, q); core.CodeOf(err) != core.Conflict {
		t.Fatal(err)
	}
	q.Generation = 1
	_, err = s.browserRequest(t.Context(), p, e.AssistantID, q)
	need(t, err)
	if calls != 2 || r.task.Browser.ViewportHeight != 1600 {
		t.Fatal(calls, r.task.Browser)
	}
}
