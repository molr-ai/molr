//go:build (linux && (amd64 || arm64)) || darwin

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
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/shellsession"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBrowserRealTaskStreamTakeoverAndPersistence(t *testing.T) { testBrowserRealTask(t, false) }
func TestBrowserFullAccessRealTaskStreamTakeoverAndPersistence(t *testing.T) {
	testBrowserRealTask(t, true)
}
func testBrowserRealTask(t *testing.T, full bool) {
	enabled := os.Getenv("MOLR_SANDBOX_INTEGRATION") == "1"
	if full {
		enabled = os.Getenv("MOLR_FULL_ACCESS_BROWSER_INTEGRATION") == "1"
	}
	if !enabled || os.Getenv("MOLR_TEST_AGENT_BROWSER") == "" || os.Getenv("MOLR_TEST_CHROMIUM") == "" {
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
	if full {
		directConfig(&cfg)
	}
	cfg.Sandbox.HostTools = runtime.GOOS == "linux" && !full
	cfg.Sandbox.CgroupRoot = os.Getenv("MOLR_SANDBOX_CGROUP")
	cfg.Browser.AgentDirectory = os.Getenv("MOLR_TEST_AGENT_BROWSER")
	cfg.Browser.ChromiumDirectory = os.Getenv("MOLR_TEST_CHROMIUM")
	b, err := Build(t.Context(), cfg)
	need(t, err)
	defer b.Close()
	defer func() {
		if t.Failed() {
			rows, _ := persistence.Scan[shellsession.Task](t.Context(), b.store, shellTasksBucket)
			for _, row := range rows {
				t.Logf("task failure: %s %s", row.Failure, row.Result)
				data, _ := os.ReadFile(filepath.Join(platform.AssistantWorkspace(cfg.DataDir, cfg.Sandbox.WorkRoot, row.OwnerID, row.AssistantID), strings.TrimPrefix(row.StderrPath, "/work/")))
				t.Logf("launch stderr: %s", data)
			}
		}
	}()

	owner, _ := ownerSession(t, b)
	e := core.Execution{Principal: core.Principal{OwnerID: owner.OwnerID, SubjectID: "browser-assistant", Kind: core.ActorAssistant, ExecutionID: "browser-run"}, AssistantID: "browser-assistant", OperationID: "browser-task"}
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "business.assistants", persistence.Key(owner.OwnerID, e.AssistantID), assistant.Assistant{ID: e.AssistantID, OwnerID: owner.OwnerID, State: assistant.Active, Version: 1}); err != nil {
			return err
		}
		return tx.Put(ctx, "agent.jobs", persistence.Key(owner.OwnerID, "browser-job"), scheduler.Job{State: scheduler.Running, ExecutionID: e.Principal.ExecutionID, Event: event.AssistantEvent{ID: "browser-job", OwnerID: owner.OwnerID, AssistantID: e.AssistantID}})
	}))
	if !full {
		need(t, b.sandbox.runner.Prepare(t.Context()))
	}
	computeResource, err := b.Services.Resources.Connect(t.Context(), owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "browser-compute-resource"}, Kind: resource.Compute, Provider: "local-sandbox", Name: "Browser compute"})
	need(t, err)
	compute := computeResource.ID
	computeLimits := &policy.ComputeConstraints{CPUPercent: 200, MemoryBytes: 2 << 30, Processes: 512, TempBytes: 512 << 20, MaxDurationSeconds: 300}
	networkTarget := "preset:host"
	if runtime.GOOS == "darwin" {
		computeLimits = &policy.ComputeConstraints{MaxDurationSeconds: 300}
		networkTarget = "preset:network"
	}
	var constraints *policy.Constraints
	if !full {
		constraints = &policy.Constraints{Compute: computeLimits}
	}
	computeGrant, err := b.Services.Grants.Grant(t.Context(), owner, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "browser-compute"}, AssistantID: e.AssistantID, ResourceID: compute, Actions: []policy.Action{policy.Execute}, Scope: func() policy.Scope {
		s := policy.Scope{FullAccess: full}
		if full {
			s.FullAccessScope = &policy.FullAccessScope{Mode: "unrestricted"}
		}
		return s
	}(), Constraints: constraints})
	need(t, err)
	network, err := b.Services.Resources.Connect(t.Context(), owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "browser-network"}, Kind: resource.Network, Provider: "sandbox-network", Name: "Fixture network", Binding: resource.Binding{SandboxTarget: networkTarget}})
	need(t, err)
	_, err = b.Services.Grants.Grant(t.Context(), owner, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "browser-network-grant"}, AssistantID: e.AssistantID, ResourceID: network.ID, Actions: []policy.Action{policy.Execute}, Constraints: testResourceConstraints(resource.Network)})
	need(t, err)
	s := b.shellTasks
	_, err = s.BrowserTool(t.Context(), e, "browser_session", tools.BrowserRequest{Action: browser.Action{Action: "start"}, ComputeID: compute, FullAccess: full})
	need(t, err)
	r := browserTask(t, s, owner, e.AssistantID)
	if r != nil {
		_, err = s.BrowserTool(t.Context(), e, "browser_session", tools.BrowserRequest{Action: browser.Action{Action: "start"}, ComputeID: compute, FullAccess: full})
		need(t, err)
		if same := browserTask(t, s, owner, e.AssistantID); same == nil || same.ID != r.ID {
			t.Fatal("same selection launched another browser")
		}
	}
	if r == nil {
		rows, _ := persistence.Scan[shellsession.Task](t.Context(), b.store, shellTasksBucket)
		for _, row := range rows {
			data, _ := os.ReadFile(filepath.Join(platform.AssistantWorkspace(cfg.DataDir, cfg.Sandbox.WorkRoot, owner.OwnerID, e.AssistantID), strings.TrimPrefix(row.StderrPath, "/work/")))
			t.Logf("browser launch stderr: %s", data)
		}
		t.Fatalf("browser not registered as a task: %+v", rows)
	}
	deadline := time.Now().Add(45 * time.Second)
	for {
		call, cancel := context.WithTimeout(t.Context(), time.Second)
		var ready json.RawMessage
		ready, err = s.BrowserReady(call, owner, e.AssistantID, r.ID)
		if err == nil && !full && runtime.GOOS == "darwin" {
			if !strings.Contains(string(ready), `"isolation":"outer-only"`) {
				t.Fatalf("Seatbelt status missing: %s", ready)
			}
		}
		if err == nil && !full && runtime.GOOS == "linux" {
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
			if full && !strings.Contains(string(ready), `"chromium_sandbox":true`) {
				t.Fatal(string(ready))
			}
			break
		}
		if time.Now().After(deadline) {
			raw, _ := s.Manage(t.Context(), owner, e.AssistantID, tools.ShellTaskRequest{Action: "read", TaskID: r.ID, Stream: "stderr"})
			t.Fatalf("browser not ready: %v stderr=%s", err, raw)
		}
		time.Sleep(100 * time.Millisecond)
	}
	action := func(a browser.Action) string {
		t.Helper()
		actor := e.Principal
		if a.Action == "screenshot" {
			actor = owner
		}
		raw, err := browserAction(t.Context(), s, actor, e.AssistantID, tools.BrowserRequest{Action: a, TaskID: r.ID})
		need(t, err)
		return string(raw)
	}
	u, _ := url.Parse(fixture.URL)
	if !full && runtime.GOOS == "linux" {
		u.Host = strings.Replace(u.Host, "127.0.0.1", "host.molr.internal", 1)
	}
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
		done <- s.BrowserFrames(streamCtx, owner, e.AssistantID, r.ID, func(raw json.RawMessage) error {
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
	_, err = browserAction(t.Context(), s, owner, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "takeover"}, TaskID: r.ID, ClientID: "test-window", Generation: 1})
	need(t, err)
	action(browser.Action{Action: "snapshot"})
	_, err = browserAction(t.Context(), s, e.Principal, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "click", Selector: "#submit"}})
	if core.ReasonOf(err) != "browser_user_controlled" {
		t.Fatal(err)
	}
	generation := browserTask(t, s, owner, e.AssistantID).Browser.Generation
	_, err = browserAction(t.Context(), s, owner, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "fill", Selector: "#name", Text: "用户输入"}, ClientID: "test-window", Generation: generation, Sequence: 1})
	need(t, err)
	batch := []browser.Action{{Action: "press", Key: "End"}, {Action: "insert_text", Text: "-batch"}, {Action: "press", Key: "Backspace"}, {Action: "wheel", X: 600, Y: 500, DeltaY: 20}}
	sequence := uint64(2)
	for len(batch) > 0 {
		raw, err := browserAction(t.Context(), s, owner, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "input_batch"}, ClientID: "test-window", Generation: generation, Sequence: sequence, Actions: batch})
		need(t, err)
		var out struct {
			Completed int
			Code      string
		}
		need(t, json.Unmarshal(raw, &out))
		if out.Code != "" || out.Completed < 1 {
			t.Fatal(string(raw))
		}
		sequence += uint64(out.Completed)
		batch = batch[out.Completed:]
	}
	if snap := action(browser.Action{Action: "snapshot"}); !strings.Contains(snap, "用户输入-batc") {
		t.Fatal("ordered native keyboard batch failed", snap)
	}
	_, err = browserAction(t.Context(), s, owner, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "release"}, ClientID: "test-window", Generation: generation})
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
	for deadline := time.Now().Add(10 * time.Second); ; {
		status, err := s.BrowserManage(t.Context(), owner, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "status"}})
		need(t, err)
		var metadata struct{ TabCount int }
		need(t, json.Unmarshal(status, &metadata))
		if metadata.TabCount == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("bad tab count: %s", status)
		}
		time.Sleep(50 * time.Millisecond)
	}
	action(browser.Action{Action: "screenshot"})
	_, err = browserAction(t.Context(), s, owner, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "close"}})
	need(t, err)
	awaitSessionEnd(t, s, owner, e.AssistantID, r.ID, 10*time.Second)
	e.OperationID = "browser-task-reopen"
	_, err = s.BrowserManage(t.Context(), owner, e.AssistantID, tools.BrowserRequest{Action: browser.Action{Action: "start"}, ComputeID: compute, FullAccess: full})
	need(t, err)
	r = browserTask(t, s, owner, e.AssistantID)
	deadline = time.Now().Add(30 * time.Second)
	for {
		call, cancel := context.WithTimeout(t.Context(), time.Second)
		_, err = s.BrowserReady(call, owner, e.AssistantID, r.ID)
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
	// Even with an idle reusable command connection, revocation terminates the
	// execution and releases its namespace, cgroup and pooled connections.
	need(t, b.Services.Grants.Revoke(t.Context(), owner, policy.RevokeRequest{GrantID: computeGrant.ID, Mutation: core.Mutation{IdempotencyKey: "browser-revoke", ExpectedVersion: computeGrant.Version}}))
	awaitSessionEnd(t, s, owner, e.AssistantID, r.ID, 5*time.Second)
	t.Log("Real browser: navigate, snapshot, fill, click, frame stream, takeover, read-only model, user input, release and close passed")
}
