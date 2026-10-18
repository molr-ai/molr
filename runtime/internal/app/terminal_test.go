package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func terminalFixture(t *testing.T) (*Bundle, core.Principal, core.Execution, *runningShell, *atomic.Int32) {
	t.Helper()
	b, owner, e := taskFixture(t)
	s := b.shellTasks
	calls := &atomic.Int32{}
	s.executeCommand = func(ctx context.Context, e core.Execution, q tools.ShellRequest) (json.RawMessage, error) {
		err := managedShellFor(ctx, e).started("", "", nil, func(ctx context.Context, q tools.ShellTaskRequest) (json.RawMessage, error) {
			if q.Action == "read" {
				return json.RawMessage(`{"Output":"prompt> ","NextCursor":8}`), nil
			}
			calls.Add(1)
			return json.RawMessage(`{"Accepted":true}`), nil
		})
		if err != nil {
			return nil, err
		}
		<-ctx.Done()
		return json.RawMessage(`{"Exit":{"Code":0}}`), nil
	}
	_, err := s.execute(t.Context(), e, tools.ShellRequest{ComputeID: "compute", TTY: true, Script: "exec /bin/sh -i"})
	need(t, err)
	s.mu.Lock()
	r := s.active[shellKey(e)]
	s.mu.Unlock()
	return b, owner, e, r, calls
}
func TestTerminalHandoffPermissionsVersionsAndReadBarrier(t *testing.T) {
	b, owner, e, r, calls := terminalFixture(t)
	s := b.shellTasks
	call := func(p core.Principal, q tools.ShellTaskRequest) error {
		q.TaskID = e.OperationID
		_, err := s.manage(t.Context(), p, e.AssistantID, q)
		return err
	}
	need(t, call(e.Principal, tools.ShellTaskRequest{Action: "write", Generation: 1, Data: "before\n"}))
	need(t, call(owner, tools.ShellTaskRequest{Action: "takeover", ClientID: "window-one", Generation: 1}))
	raw, err := s.manage(t.Context(), owner, e.AssistantID, tools.ShellTaskRequest{Action: "list"})
	need(t, err)
	var listing struct{ Items []shellTask }
	need(t, json.Unmarshal(raw, &listing))
	if len(listing.Items) != 1 || !listing.Items[0].Terminal.ControllerIsCurrentSession || listing.Items[0].Terminal.ControllerClientID != "window-one" {
		t.Fatal("list lost active controller", string(raw))
	}

	for _, action := range []string{"write", "interrupt", "resize", "stop"} {
		if err := call(e.Principal, tools.ShellTaskRequest{Action: action, Generation: 2, Data: "denied"}); core.ReasonOf(err) != "terminal_user_controlled" {
			t.Fatalf("%s: %v", action, err)
		}
	}
	need(t, call(e.Principal, tools.ShellTaskRequest{Action: "stop_all"}))
	select {
	case <-r.done:
		t.Fatal("assistant stop_all killed user terminal")
	default:
	}
	if err := call(owner, tools.ShellTaskRequest{Action: "write", ClientID: "window-two", Generation: 2, Sequence: 1, Data: "denied"}); err == nil {
		t.Fatal("other window wrote")
	}
	need(t, call(owner, tools.ShellTaskRequest{Action: "write", ClientID: "window-one", Generation: 2, Sequence: 1, Data: "user\n"}))
	if err := call(owner, tools.ShellTaskRequest{Action: "write", ClientID: "window-one", Generation: 2, Sequence: 1, Data: "duplicate"}); err == nil {
		t.Fatal("duplicate input accepted")
	}
	// Another window may explicitly take over; the old window is fenced.
	need(t, call(owner, tools.ShellTaskRequest{Action: "takeover", ClientID: "window-two", Generation: 2}))
	if err := call(owner, tools.ShellTaskRequest{Action: "release", ClientID: "window-one", Generation: 3}); err == nil {
		t.Fatal("other window returned control")
	}
	need(t, call(owner, tools.ShellTaskRequest{Action: "release", ClientID: "window-two", Generation: 3}))
	if err := call(e.Principal, tools.ShellTaskRequest{Action: "write", Generation: 1, Data: "stale"}); core.CodeOf(err) != core.Conflict {
		t.Fatalf("stale generation: %v", err)
	}
	if err := call(e.Principal, tools.ShellTaskRequest{Action: "write", Generation: 4, Data: "unread"}); core.ReasonOf(err) != "terminal_read_required" {
		t.Fatal(err)
	}
	need(t, call(owner, tools.ShellTaskRequest{Action: "read", Generation: 4}))
	if err := call(e.Principal, tools.ShellTaskRequest{Action: "write", Generation: 4}); err == nil {
		t.Fatal("owner read cleared assistant barrier")
	}
	need(t, call(e.Principal, tools.ShellTaskRequest{Action: "read", Generation: 4}))
	need(t, call(e.Principal, tools.ShellTaskRequest{Action: "write", Generation: 4, Data: "after\n"}))
	need(t, call(e.Principal, tools.ShellTaskRequest{Action: "stop_all"}))
	select {
	case <-r.done:
	case <-time.After(time.Second):
		t.Fatal("assistant stop_all did not stop its terminal")
	}
	if calls.Load() != 3 {
		t.Fatalf("unexpected writes: %d", calls.Load())
	}
	events, err := persistence.Scan[event.AssistantEvent](t.Context(), b.store, "agent.events")
	need(t, err)
	returned := 0
	for _, ev := range events {
		if ev.Kind == event.TerminalControlReturned {
			returned++
			if ev.DeliveryMode != event.WhenIdle {
				t.Fatal(ev)
			}
		}
	}
	if returned != 1 {
		t.Fatal("return event missing", returned)
	}
	foreign := owner
	foreign.OwnerID = "foreign"
	if err := call(foreign, tools.ShellTaskRequest{Action: "read"}); err == nil {
		t.Fatal("foreign owner read")
	}
}
func TestTerminalManualStartUsesAssistantCapabilityAndSuppressesReminders(t *testing.T) {
	b, owner, e := taskFixture(t)
	s := b.shellTasks
	seen := make(chan core.Execution, 1)
	s.executeCommand = func(ctx context.Context, execution core.Execution, q tools.ShellRequest) (json.RawMessage, error) {
		if execution.Principal.Kind != core.ActorAssistant || execution.AssistantID != e.AssistantID || q.FullAccess || !q.TTY {
			t.Error("wrong execution scope")
		}
		if err := (access{b.store}).execution(ctx, execution); err != nil {
			t.Error(err)
		}
		need(t, managedShellFor(ctx, execution).started("", "", nil, func(context.Context, tools.ShellTaskRequest) (json.RawMessage, error) {
			return json.RawMessage(`{"Accepted":true}`), nil
		}))
		seen <- execution
		<-ctx.Done()
		return json.RawMessage(`{"Exit":{"Code":0}}`), nil
	}
	raw := json.RawMessage(`{"request_id":"manual-terminal","client_id":"window","compute_id":"compute"}`)
	_, err := s.startTerminal(t.Context(), owner, e.AssistantID, raw)
	need(t, err)
	execution := <-seen
	_, err = s.startTerminal(t.Context(), owner, e.AssistantID, raw)
	need(t, err)
	select {
	case <-seen:
		t.Fatal("launch replayed")
	default:
	}
	s.mu.Lock()
	r := s.active[shellKey(execution)]
	view := terminalView(r.task.Terminal, owner)
	if view.Control != "user" || !view.ControllerIsCurrentSession || view.ControllerClientID != "window" || r.task.CreatedBy != "user" || len(s.reminders) != 0 {
		t.Fatal("manual controller/reminders", view)
	}
	s.mu.Unlock()
	need(t, s.tick(t.Context(), time.Now().Add(10*time.Minute)))
	events, err := persistence.Scan[event.AssistantEvent](t.Context(), b.store, "agent.events")
	need(t, err)
	for _, ev := range events {
		if ev.Kind == event.ShellTasksRunning {
			t.Fatal("manual terminal woke assistant")
		}
	}
	_, err = s.startTerminal(t.Context(), owner, e.AssistantID, json.RawMessage(`{"request_id":"escape","client_id":"window","compute_id":"compute","full_access":true}`))
	if err == nil {
		t.Fatal("extra launch scope accepted")
	}
}
func TestTerminalOutputPreservesBytesAcrossChunks(t *testing.T) {
	root := t.TempDir()
	prefix := ".molr/tool-shell-test"
	// Use the same archive naming as the runtime rather than a second protocol.
	e := core.Execution{Principal: core.Principal{OwnerID: "owner"}, OperationID: "bytes"}
	prefix = shellOutputPrefix(e)
	out, stderr, path, errpath, err := createShellOutputsNamed(root, 1<<20, prefix)
	need(t, err)
	payload := []byte(strings.Repeat("a", (16<<10)-1) + "中文\x1b[31m\xff")
	_, err = out.Write(payload)
	need(t, err)
	out.close()
	stderr.close()
	var got []byte
	var cursor int64
	for len(got) < len(payload) {
		raw, err := readShellOutput(root, path, errpath, tools.ShellTaskRequest{Action: "read", Encoding: "base64", Cursor: cursor})
		need(t, err)
		var chunk struct {
			Output     string
			NextCursor int64
		}
		need(t, json.Unmarshal(raw, &chunk))
		bytes, err := base64.StdEncoding.DecodeString(chunk.Output)
		need(t, err)
		got = append(got, bytes...)
		cursor = chunk.NextCursor
	}
	if string(got) != string(payload) {
		t.Fatal("terminal bytes changed")
	}
}
