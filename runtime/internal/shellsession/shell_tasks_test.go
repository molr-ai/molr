package shellsession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type failedStartStore struct {
	persistence.Store
	failNext atomic.Bool
}

func (s *failedStartStore) Write(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	if s.failNext.Swap(false) {
		return core.NewError(core.Unavailable, "start write failed")
	}
	return s.Store.Write(ctx, fn)
}

func TestShellStartedSaveFailureCancelsBeforeReady(t *testing.T) {
	b, _, e := taskFixture(t)
	s := b.shellTasks
	store := &failedStartStore{Store: s.store}
	s.store = store
	s.executeCommand = func(ctx context.Context, e core.Execution, q tools.ShellRequest) (json.RawMessage, error) {
		store.failNext.Store(true)
		err := ManagedFor(ctx, e).Started("", "", nil, nil)
		if core.CodeOf(err) != core.Unavailable || ctx.Err() != context.Canceled {
			t.Errorf("failed start: err=%v, context=%v", err, ctx.Err())
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		select {
		case <-s.active[shellKey(e)].ready:
			t.Error("failed start marked the terminal ready")
		default:
		}
		return nil, err
	}
	_, err := s.Execute(t.Context(), e, tools.ShellRequest{ComputeID: "compute", TTY: true, Script: "wait"})
	if core.CodeOf(err) != core.Unavailable {
		t.Fatalf("terminal start did not return the save failure: %v", err)
	}
}

func TestShellTasksDetachLimitOwnershipAndEvents(t *testing.T) {
	b, p, e := taskFixture(t)
	s := b.shellTasks
	release := make(chan struct{})
	started := make(chan context.Context, 16)
	s.executeCommand = func(ctx context.Context, e core.Execution, q tools.ShellRequest) (json.RawMessage, error) {
		_ = ManagedFor(ctx, e).Started("", "", nil, nil)
		started <- ctx
		select {
		case <-release:
		case <-ctx.Done():
		}
		return json.RawMessage(`{"Exit":{"Code":0}}`), nil
	}
	var first time.Time
	for i := 0; i < 10; i++ {
		e.OperationID = core.ID(fmt.Sprintf("op%d", i))
		req := tools.ShellRequest{ComputeID: "compute", Script: "sleep 100", Background: i != 1, TTY: i == 1}
		raw, err := s.Execute(t.Context(), e, req)
		need(t, err)
		if !json.Valid(raw) {
			t.Fatal(string(raw))
		}
		<-started
		s.mu.Lock()
		due := s.reminders[persistence.Key(p.OwnerID, e.AssistantID)]
		s.mu.Unlock()
		if i == 0 {
			first = due
		} else if !due.Equal(first) {
			t.Fatal("new task postponed reminder")
		}
	}
	e.OperationID = "overflow"
	_, err := s.Execute(t.Context(), e, tools.ShellRequest{ComputeID: "compute", Script: "true"})
	if core.CodeOf(err) != core.RateLimited {
		t.Fatal(err)
	}
	foreign := e.Principal
	foreign.SubjectID = "another"
	_, err = s.Manage(t.Context(), foreign, e.AssistantID, tools.ShellTaskRequest{Action: "stop_all"})
	if err == nil {
		t.Fatal("cross-assistant management allowed")
	}
	need(t, s.tick(t.Context(), first))
	events, err := persistence.Scan[event.AssistantEvent](t.Context(), b.store, "agent.events")
	need(t, err)
	reminders := 0
	for _, event := range events {
		if event.Kind == "shell_tasks_running" {
			reminders++
			var d struct{ Tasks []any }
			need(t, json.Unmarshal(event.Details, &d))
			if len(d.Tasks) != 10 {
				t.Fatal(len(d.Tasks))
			}
		}
	}
	if reminders != 1 {
		t.Fatal(reminders)
	}
	need(t, s.tick(t.Context(), first.Add(5*time.Minute)))
	events, err = persistence.Scan[event.AssistantEvent](t.Context(), b.store, "agent.events")
	need(t, err)
	if len(events) != 1 {
		t.Fatal("queued reminder not coalesced", len(events))
	}
	// Original loop ended; the process capability remains valid but cannot be used
	// by arbitrary contexts or after the manager releases the task.
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Delete(ctx, "agent.jobs", persistence.Key(p.OwnerID, "shell-job"))
	}))
	if s.host.CheckExecution(t.Context(), e) == nil {
		t.Fatal("ended loop remains authorized")
	}
	close(release)
	s.wg.Wait()
	events, err = persistence.Scan[event.AssistantEvent](t.Context(), b.store, "agent.events")
	need(t, err)
	completed := 0
	for _, ev := range events {
		if ev.Kind == event.ShellTaskFinished {
			completed++
		}
	}
	if completed != 9 {
		t.Fatalf("expected only background completion events: %d", completed)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.active) != 0 || len(s.reminders) != 0 {
		t.Fatal("slots or reminder leaked")
	}
}
func TestShellForegroundPromotionAndCancellation(t *testing.T) {
	b, _, e := taskFixture(t)
	s := b.shellTasks
	s.foregroundWait = 20 * time.Millisecond
	running := make(chan context.Context, 1)
	s.executeCommand = func(ctx context.Context, e core.Execution, q tools.ShellRequest) (json.RawMessage, error) {
		running <- ctx
		<-ctx.Done()
		return json.RawMessage(`{"Exit":{"Code":125,"Reason":"canceled"}}`), nil
	}
	call, cancel := context.WithCancel(t.Context())
	raw, err := s.Execute(call, e, tools.ShellRequest{ComputeID: "compute", Script: "sleep 60"})
	need(t, err)
	var receipt struct {
		Task   Task
		Notice string
	}
	need(t, json.Unmarshal(raw, &receipt))
	if receipt.Task.Mode != "background" || receipt.Task.ID != e.OperationID || receipt.Notice == "" {
		t.Fatal(string(raw))
	}
	run := <-running
	cancel()
	if run.Err() != nil {
		t.Fatal("returning call canceled detached task")
	}
	if err := s.host.CheckExecution(run, e); err != nil {
		t.Fatal(err)
	}
	_, err = s.Manage(t.Context(), e.Principal, e.AssistantID, tools.ShellTaskRequest{Action: "stop", TaskID: e.OperationID})
	need(t, err)
	s.wg.Wait()
	if ManagedFor(run, e) != nil {
		t.Fatal("completed capability alive")
	}
}
func TestShellRestartInterruptsWithoutReplay(t *testing.T) {
	b, p, e := taskFixture(t)
	s := b.shellTasks
	for _, mode := range []string{"foreground", "background", "tty"} {
		need(t, s.save(t.Context(), Task{ID: core.ID(mode), OwnerID: p.OwnerID, AssistantID: e.AssistantID, Mode: mode, State: "running", StartedAt: time.Now()}))
	}
	ctx, cancel := context.WithCancel(t.Context())
	need(t, s.Start(ctx))
	cancel()
	s.wg.Wait()
	rows, err := persistence.Scan[Task](t.Context(), b.store, shellTasksBucket)
	need(t, err)
	for _, r := range rows {
		if r.State != "interrupted" {
			t.Fatal(r)
		}
	}
	events, err := persistence.Scan[event.AssistantEvent](t.Context(), b.store, "agent.events")
	need(t, err)
	if len(events) != 1 || events[0].OperationID != "background" {
		t.Fatal(events)
	}
}

func TestShellForegroundUsesSameAssistantLimit(t *testing.T) {
	b, _, e := taskFixture(t)
	s := b.shellTasks
	s.foregroundWait = time.Hour
	release := make(chan struct{})
	started := make(chan struct{}, 10)
	s.executeCommand = func(ctx context.Context, e core.Execution, q tools.ShellRequest) (json.RawMessage, error) {
		_ = ManagedFor(ctx, e).Started("", "", nil, nil)
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return json.RawMessage(`{"Exit":{"Code":0}}`), nil
	}
	front := make(chan error, 1)
	go func() {
		_, err := s.Execute(t.Context(), e, tools.ShellRequest{ComputeID: "compute", Script: "sleep 10"})
		front <- err
	}()
	<-started
	for i := 0; i < 9; i++ {
		q := e
		q.OperationID = core.ID(fmt.Sprintf("background%d", i))
		_, err := s.Execute(t.Context(), q, tools.ShellRequest{ComputeID: "compute", Script: "sleep 10", Background: true})
		need(t, err)
		<-started
	}
	q := e
	q.OperationID = "overflow"
	_, err := s.Execute(t.Context(), q, tools.ShellRequest{ComputeID: "compute", Script: "true", TTY: true})
	if core.CodeOf(err) != core.RateLimited {
		t.Fatal(err)
	}
	close(release)
	need(t, <-front)
	s.wg.Wait()
}

func TestShellReminderAfterLoopStopHasFreshEventIdentity(t *testing.T) {
	b, p, e := taskFixture(t)
	now := time.Now().UTC()
	data := json.RawMessage(`{"Tasks":[{"ID":"running"}]}`)
	need(t, appendShellReminder(t.Context(), b.store, p.OwnerID, e.AssistantID, now, data))
	events, err := persistence.Scan[event.AssistantEvent](t.Context(), b.store, "agent.events")
	need(t, err)
	old := events[0]
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "agent.jobs", persistence.Key(p.OwnerID, old.ID), scheduler.Job{Event: old, State: scheduler.Queued, Version: 1}); err != nil {
			return err
		}
		return tx.Put(ctx, "agent.interrupt_boundaries", persistence.Key(p.OwnerID, e.AssistantID), event.InterruptBoundary{At: now.Add(time.Minute)})
	}))
	need(t, appendShellReminder(t.Context(), b.store, p.OwnerID, e.AssistantID, now.Add(5*time.Minute), data))
	events, err = persistence.Scan[event.AssistantEvent](t.Context(), b.store, "agent.events")
	need(t, err)
	if len(events) != 1 || events[0].ID == old.ID || !events[0].CreatedAt.Equal(now.Add(5*time.Minute)) {
		t.Fatal(events)
	}
	job, err := persistence.Load[scheduler.Job](t.Context(), b.store, "agent.jobs", persistence.Key(p.OwnerID, old.ID))
	need(t, err)
	if job.State != scheduler.Canceled {
		t.Fatal(job)
	}
}

func TestShellTaskListBoundsHistoryKeepsRunningAndScopesAssistant(t *testing.T) {
	b, p, e := taskFixture(t)
	now := time.Now().UTC()
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < 120; i++ {
			id := core.ID(fmt.Sprintf("bounded-%03d", i))
			state := "completed"
			assistantID := e.AssistantID
			if i < 3 {
				state = "running"
			}
			if i >= 110 {
				assistantID = "other"
			}
			// Vary offsets: sorting must use normalized time, not JSON lexical order.
			at := now.Add(time.Duration(i) * time.Second).In(time.FixedZone("offset", (i%3-1)*3600))
			if err := tx.Put(ctx, shellTasksBucket, persistence.Key(p.OwnerID, id), Task{ID: id, OwnerID: p.OwnerID, AssistantID: assistantID, State: state, StartedAt: at, Result: json.RawMessage(`{"Stdout":"private output","Stderr":"private error","OutputComplete":true}`)}); err != nil {
				return err
			}
		}
		return nil
	}))
	raw, err := b.shellTasks.Manage(t.Context(), p, e.AssistantID, tools.ShellTaskRequest{Action: "list"})
	need(t, err)
	var result struct{ Items []Task }
	need(t, json.Unmarshal(raw, &result))
	if len(result.Items) != 53 {
		t.Fatalf("got %d tasks", len(result.Items))
	}
	for i, item := range result.Items {
		if item.AssistantID != e.AssistantID {
			t.Fatal("other assistant leaked")
		}
		if i < 3 && item.State != "running" || i >= 3 && item.State == "running" {
			t.Fatal("incorrect ordering")
		}
		if i > 3 && item.StartedAt.After(result.Items[i-1].StartedAt) {
			t.Fatal("history out of order")
		}
		var meta map[string]any
		need(t, json.Unmarshal(item.Result, &meta))
		if _, ok := meta["Stdout"]; ok {
			t.Fatal("stdout leaked in list")
		}
	}
	if result.Items[3].ID != "bounded-109" || result.Items[52].ID != "bounded-060" {
		t.Fatal("incorrect history window")
	}
}

func TestShellTaskLifecycleEventsAreScopedAndTransactional(t *testing.T) {
	b, p, e := taskFixture(t)
	task := Task{ID: "lifecycle-task", OwnerID: p.OwnerID, AssistantID: e.AssistantID, Mode: "foreground", State: "running", StartedAt: time.Now().UTC()}
	need(t, b.shellTasks.save(t.Context(), task))
	task.StdoutPath = "/work/private-output"
	need(t, b.shellTasks.save(t.Context(), task))
	events, err := persistence.Select[struct {
		Topic  string
		Object core.Object
	}](t.Context(), b.store, "im.visible", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID), persistence.Eq("Topic", "shell.tasks")}})
	need(t, err)
	if len(events) != 1 || events[0].Object.ID != e.AssistantID {
		t.Fatal(events)
	}
	task.State = "completed"
	need(t, b.shellTasks.finish(t.Context(), task))
	need(t, b.shellTasks.finish(t.Context(), task))
	err = b.store.Write(t.Context(), func(ctx context.Context, _ persistence.Tx) error {
		task.State = "failed"
		if err := b.shellTasks.save(ctx, task); err != nil {
			return err
		}
		return errors.New("rollback fixture")
	})
	if err == nil {
		t.Fatal("expected rollback")
	}
	saved, err := persistence.Load[Task](t.Context(), b.store, shellTasksBucket, persistence.Key(p.OwnerID, task.ID))
	need(t, err)
	if saved.State != "completed" {
		t.Fatal("task escaped rollback")
	}
	events, err = persistence.Select[struct {
		Topic  string
		Object core.Object
	}](t.Context(), b.store, "im.visible", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID), persistence.Eq("Topic", "shell.tasks")}})
	need(t, err)
	if len(events) != 2 {
		t.Fatal(events)
	}
}

func TestFinishedShellOutputStopsAtRetainedEOF(t *testing.T) {
	b, p, e := taskFixture(t)
	s := b.shellTasks
	root := s.host.Workspace(p.OwnerID, e.AssistantID)
	relative := platform.WorkspaceStateDirectory + "/tool-shell-retained/stdout"
	need(t, os.MkdirAll(filepath.Dir(filepath.Join(root, relative)), 0700))
	need(t, os.WriteFile(filepath.Join(root, relative), []byte("retained output"), 0600))
	task := Task{ID: "retained-output", OwnerID: p.OwnerID, AssistantID: e.AssistantID, State: "completed", SandboxID: "local", StdoutPath: "/work/" + relative, Result: json.RawMessage(`{"OutputComplete":false}`)}
	need(t, s.save(t.Context(), task))
	var output struct {
		Output                   string
		NextCursor               int64
		Finished, OutputComplete bool
	}
	raw, err := s.Manage(t.Context(), p, e.AssistantID, tools.ShellTaskRequest{Action: "read", TaskID: task.ID})
	need(t, err)
	need(t, json.Unmarshal(raw, &output))
	if output.Output != "retained output" || !output.Finished || output.OutputComplete {
		t.Fatal(output)
	}
	raw, err = s.Manage(t.Context(), p, e.AssistantID, tools.ShellTaskRequest{Action: "read", TaskID: task.ID, Cursor: output.NextCursor})
	need(t, err)
	need(t, json.Unmarshal(raw, &output))
	if output.Output != "" || !output.Finished {
		t.Fatal(output)
	}
}
