//go:build linux && (amd64 || arm64)

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/browser"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestBrowserRealTaskStreamTakeoverAndPersistence(t *testing.T) {
	if os.Getenv("MOLR_SANDBOX_INTEGRATION") != "1" || os.Getenv("MOLR_TEST_AGENT_BROWSER") == "" || os.Getenv("MOLR_TEST_CHROMIUM") == "" {
		t.Skip("requires real sandbox and installed browser programs")
	}
	exe, err := os.Executable()
	need(t, err)
	need(t, os.Chmod(exe, 0755))
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><title>Browser task acceptance</title><label>Name <input id="name"></label><button id="submit" onclick="document.querySelector('#result').textContent='Hello '+document.querySelector('#name').value;localStorage.setItem('molr-test','saved')">Submit</button><p id="result"></p><p id="persisted"></p><script>document.querySelector("#persisted").textContent=localStorage.getItem("molr-test")||"empty"</script>`)
	}))
	defer fixture.Close()
	cfg := testConfig(t)
	cfg.Sandbox.HostTools = true
	cfg.Sandbox.CgroupRoot = os.Getenv("MOLR_SANDBOX_CGROUP")
	cfg.Browser.AgentDirectory = os.Getenv("MOLR_TEST_AGENT_BROWSER")
	cfg.Browser.ChromiumDirectory = os.Getenv("MOLR_TEST_CHROMIUM")
	b, err := Build(t.Context(), cfg)
	need(t, err)
	defer b.Close()
	owner, _ := ownerSession(t, b)
	e := core.Execution{Principal: core.Principal{OwnerID: owner.OwnerID, SubjectID: "browser-assistant", Kind: core.ActorAssistant, ExecutionID: "browser-run"}, AssistantID: "browser-assistant", OperationID: "browser-task"}
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "business.assistants", persistence.Key(owner.OwnerID, e.AssistantID), assistant.Assistant{ID: e.AssistantID, OwnerID: owner.OwnerID, State: assistant.Active, Version: 1}); err != nil {
			return err
		}
		return tx.Put(ctx, "agent.jobs", persistence.Key(owner.OwnerID, "browser-job"), scheduler.Job{State: scheduler.Running, ExecutionID: e.Principal.ExecutionID, Event: event.AssistantEvent{ID: "browser-job", OwnerID: owner.OwnerID, AssistantID: e.AssistantID}})
	}))
	compute := forwardCompute(t, b, owner, false)
	_, err = b.Services.Grants.Grant(t.Context(), owner, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "browser-compute"}, AssistantID: e.AssistantID, ResourceID: compute, Actions: []policy.Action{policy.Execute}, Constraints: &policy.Constraints{Compute: &policy.ComputeConstraints{CPUPercent: 200, MemoryBytes: 2 << 30, Processes: 512, TempBytes: 512 << 20, MaxDurationSeconds: 300}}})
	need(t, err)
	network, err := b.Services.Resources.Connect(t.Context(), owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "browser-network"}, Kind: resource.Network, Provider: "sandbox-network", Name: "Fixture network", Binding: resource.Binding{SandboxTarget: "preset:host"}})
	need(t, err)
	_, err = b.Services.Grants.Grant(t.Context(), owner, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "browser-network-grant"}, AssistantID: e.AssistantID, ResourceID: network.ID, Actions: []policy.Action{policy.Execute}, Constraints: testResourceConstraints(resource.Network)})
	need(t, err)
	s := b.shellTasks
	_, err = s.browserTool(t.Context(), e, "browser_session", tools.BrowserRequest{Action: browser.Action{Action: "start"}, ComputeID: compute})
	need(t, err)
	r := s.activeBrowser(owner, e.AssistantID, "")
	if r == nil {
		t.Fatal("browser not registered as a task")
	}
	deadline := time.Now().Add(45 * time.Second)
	for {
		call, cancel := context.WithTimeout(t.Context(), time.Second)
		var ready json.RawMessage
		ready, err = s.browserCall(call, r, "GET", "/ready", nil)
		if err == nil {
			if !strings.Contains(string(ready), `Layer 1 Sandbox\tNamespace`) {
				t.Fatalf("native namespace sandbox missing: %s", ready)
			}
			for _, flag := range []string{"PID namespaces", "Network namespaces", "Seccomp-BPF sandbox", "TSYNC"} {
				if !regexp.MustCompile(regexp.QuoteMeta(flag) + `(?:\\t|\s)+Yes`).Match(ready) {
					t.Fatalf("native sandbox status missing %s: %s", flag, ready)
				}
			}
		}
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			raw, _ := s.manage(t.Context(), owner, e.AssistantID, tools.ShellTaskRequest{Action: "read", TaskID: r.task.ID, Stream: "stderr"})
			t.Fatalf("browser not ready: %v stderr=%s", err, raw)
		}
		time.Sleep(100 * time.Millisecond)
	}
	action := func(a browser.Action) string {
		t.Helper()
		raw, err := s.browserRequest(t.Context(), e.Principal, e.AssistantID, tools.BrowserRequest{Action: a, TaskID: r.task.ID})
		need(t, err)
		return string(raw)
	}
	u, _ := url.Parse(fixture.URL)
	u.Host = strings.Replace(u.Host, "127.0.0.1", "host.molr.internal", 1)
	action(browser.Action{Action: "navigate", URL: u.String()})
	if snap := action(browser.Action{Action: "snapshot"}); !strings.Contains(snap, "Name") {
		t.Fatal(snap)
	}
	action(browser.Action{Action: "fill", Selector: "#name", Text: "--cdp=9222 中文"})
	action(browser.Action{Action: "click", Selector: "#submit"})
	if snap := action(browser.Action{Action: "snapshot"}); !strings.Contains(snap, "Hello --cdp=9222 中文") {
		t.Fatal(snap)
	}
	streamCtx, stopStream := context.WithTimeout(t.Context(), 10*time.Second)
	defer stopStream()
	frames := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- s.browserFrames(streamCtx, owner, e.AssistantID, r.task.ID, func(raw json.RawMessage) error {
			if strings.Contains(string(raw), `"type":"frame"`) {
				select {
				case frames <- struct{}{}:
				default:
				}
			}
			return nil
		})
	}()
	select {
	case <-frames:
	case err := <-done:
		t.Fatal("stream ended", err)
	case <-streamCtx.Done():
		t.Fatal("no frame received")
	}
	stopStream()
	<-done
	_, err = s.browserRequest(t.Context(), owner, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "takeover"}, TaskID: r.task.ID, ClientID: "test-window", Generation: 1})
	need(t, err)
	action(browser.Action{Action: "snapshot"})
	_, err = s.browserRequest(t.Context(), e.Principal, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "click", Selector: "#submit"}})
	if core.ReasonOf(err) != "browser_user_controlled" {
		t.Fatal(err)
	}
	s.mu.Lock()
	generation := r.task.Browser.Generation
	s.mu.Unlock()
	_, err = s.browserRequest(t.Context(), owner, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "fill", Selector: "#name", Text: "用户输入"}, ClientID: "test-window", Generation: generation, Sequence: 1})
	need(t, err)
	_, err = s.browserRequest(t.Context(), owner, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "release"}, ClientID: "test-window", Generation: generation})
	need(t, err)
	action(browser.Action{Action: "snapshot"})
	action(browser.Action{Action: "click", Selector: "#submit"})
	if snap := action(browser.Action{Action: "snapshot"}); !strings.Contains(snap, "用户输入") {
		t.Fatal(snap)
	}
	action(browser.Action{Action: "new_tab", URL: "about:blank"})
	action(browser.Action{Action: "select_tab", Tab: "t1"})
	if snap := action(browser.Action{Action: "snapshot"}); !strings.Contains(snap, "用户输入") {
		t.Fatal("tab switch failed", snap)
	}
	// Close the inactive tab by stable ID, preserving the current page.
	action(browser.Action{Action: "close_tab", Tab: "t2"})
	if snap := action(browser.Action{Action: "snapshot"}); !strings.Contains(snap, "用户输入") {
		t.Fatal("closed the active tab instead", snap)
	}
	s.mu.Lock()
	r.browserTabsAt = time.Time{}
	s.mu.Unlock()
	status, err := s.browserStatus(t.Context(), owner, e.AssistantID)
	need(t, err)
	var metadata struct{ TabCount int }
	need(t, json.Unmarshal(status, &metadata))
	if metadata.TabCount != 1 {
		t.Fatalf("bad tab count: %s", status)
	}
	action(browser.Action{Action: "screenshot"})
	_, err = s.browserRequest(t.Context(), owner, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "close"}})
	need(t, err)
	select {
	case <-r.done:
	case <-time.After(10 * time.Second):
		t.Fatal("browser did not stop")
	}
	e.OperationID = "browser-task-reopen"
	_, err = s.browserTool(t.Context(), e, "browser_session", tools.BrowserRequest{Action: browser.Action{Action: "start"}, ComputeID: compute})
	need(t, err)
	r = s.activeBrowser(owner, e.AssistantID, "")
	deadline = time.Now().Add(30 * time.Second)
	for {
		call, cancel := context.WithTimeout(t.Context(), time.Second)
		_, err = s.browserCall(call, r, "GET", "/ready", nil)
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reopened browser not ready", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	action(browser.Action{Action: "navigate", URL: u.String()})
	if snap := action(browser.Action{Action: "snapshot"}); !strings.Contains(snap, "saved") {
		t.Fatal("profile state did not persist", snap)
	}
	r.cancel()
	<-r.done
	t.Log("Real browser: navigate, snapshot, fill, click, frame stream, takeover, read-only model, user input, release and close passed")
}
