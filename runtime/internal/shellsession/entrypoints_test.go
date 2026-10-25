package shellsession

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/browser"
	"molr.ai/runtime/internal/core"
	"net/http"
	"strings"
	"testing"
)

type diagnosticTransport func(*http.Request) (*http.Response, error)

func (f diagnosticTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBrowserEntryPointsKeepOwnershipAndFixedDiagnostics(t *testing.T) {
	s, p, e, r := browserTaskFixture(t)
	calls := 0
	r.browserHTTPOnce.Do(func() {
		r.browserHTTP = &http.Client{Transport: diagnosticTransport(func(req *http.Request) (*http.Response, error) {
			calls++
			if req.Method != "GET" || req.URL.Path != "/ready" {
				t.Fatal("unexpected diagnostic request", req.Method, req.URL.Path)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ready":true}`)), Header: make(http.Header)}, nil
		})}
	})
	data, err := s.BrowserReady(t.Context(), p, e.AssistantID, r.task.ID)
	need(t, err)
	if string(data) != `{"ready":true}` {
		t.Fatal(string(data))
	}
	if _, err := s.BrowserReady(t.Context(), e.Principal, e.AssistantID, r.task.ID); err == nil {
		t.Fatal("assistant read owner diagnostics")
	}
	if _, err := s.BrowserReady(t.Context(), p, "other", r.task.ID); err == nil {
		t.Fatal("foreign assistant read diagnostics")
	}
	if calls != 1 {
		t.Fatal("unauthorized diagnostics reached browser", calls)
	}
	_, err = s.BrowserTool(t.Context(), e, "browser_action", tools.BrowserRequest{Action: browser.Action{Action: "snapshot"}})
	need(t, err)
	_, err = s.BrowserManage(t.Context(), p, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "takeover"}, ClientID: "window", Generation: 1})
	need(t, err)
	if _, err = s.BrowserTool(t.Context(), e, "browser_action", tools.BrowserRequest{Action: browser.Action{Action: "click", Selector: "#button"}}); core.ReasonOf(err) != "browser_user_controlled" {
		t.Fatal(err)
	}
	if _, err = s.BrowserManage(t.Context(), e.Principal, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "status"}}); err == nil {
		t.Fatal("assistant used owner entry")
	}
}

func TestTerminalEntryPointsPreserveStreamBackpressureAndIdentity(t *testing.T) {
	b, p, e, r, _ := terminalFixture(t)
	s := b.shellTasks
	raw, err := s.Tool(t.Context(), e, tools.ShellTaskRequest{Action: "list"})
	need(t, err)
	if !strings.Contains(string(raw), string(e.OperationID)) {
		t.Fatal("tool list lost task", string(raw))
	}
	forged := e
	forged.Principal.ExecutionID = "ended"
	if _, err = s.Tool(t.Context(), forged, tools.ShellTaskRequest{Action: "list"}); err == nil {
		t.Fatal("ended execution listed tasks")
	}
	s.mu.Lock()
	r.scope.control = func(_ context.Context, q tools.ShellTaskRequest) (json.RawMessage, error) {
		if q.Encoding != "base64" || q.Cursor != 7 {
			t.Fatal("stream changed byte protocol", q)
		}
		return json.RawMessage(`{"Output":"QQ==","NextCursor":8}`), nil
	}
	s.mu.Unlock()
	stopped := errors.New("consumer stopped")
	err = s.TerminalFrames(t.Context(), p, e.AssistantID, e.OperationID, 7, func(data json.RawMessage) error {
		if !strings.Contains(string(data), `"Output":"QQ=="`) {
			t.Fatal(string(data))
		}
		return stopped
	})
	if !errors.Is(err, stopped) || len(s.forwardSlots) != 0 {
		t.Fatal("stream lost consumer error or retained slot", err)
	}
	if err = s.TerminalFrames(t.Context(), e.Principal, e.AssistantID, e.OperationID, 0, func(json.RawMessage) error { return nil }); err == nil {
		t.Fatal("assistant used owner stream")
	}
}
