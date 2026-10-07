package app

import (
	"context"
	"encoding/json"
	"fmt"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"testing"
	"time"
)

func taskFixture(t *testing.T) (*Bundle, core.Principal, core.Execution) {
	t.Helper()
	b, err := Build(t.Context(), testConfig(t))
	need(t, err)
	t.Cleanup(func() { _ = b.Close() })
	p, _ := ownerSession(t, b)
	need(t, resource.NewRepository(b.store).Insert(t.Context(), resource.Resource{ID: "compute", OwnerID: p.OwnerID, Kind: resource.Compute, Provider: "managed-compute", State: resource.Available, Version: 1}, resource.Binding{ResourceID: "compute"}))
	e := core.Execution{Principal: core.Principal{OwnerID: p.OwnerID, SubjectID: "shell-assistant", Kind: core.ActorAssistant, ExecutionID: "shell-run"}, AssistantID: "shell-assistant", OperationID: "shell-operation"}
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "business.assistants", persistence.Key(p.OwnerID, e.AssistantID), assistant.Assistant{ID: e.AssistantID, OwnerID: p.OwnerID, State: assistant.Active, Version: 1}); err != nil {
			return err
		}
		return tx.Put(ctx, "agent.jobs", persistence.Key(p.OwnerID, "shell-job"), scheduler.Job{State: scheduler.Running, ExecutionID: e.Principal.ExecutionID, Event: event.AssistantEvent{ID: "shell-job", OwnerID: p.OwnerID, AssistantID: e.AssistantID}})
	}))
	return b, p, e
}
func TestShellTasksDetachLimitOwnershipAndEvents(t *testing.T) {
	b, p, e := taskFixture(t)
	s := b.shellTasks
	release := make(chan struct{})
	started := make(chan context.Context, 16)
	s.executeCommand = func(ctx context.Context, e core.Execution, q tools.ShellRequest) (json.RawMessage, error) {
		_ = managedShellFor(ctx, e).started("", "", nil)
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
		raw, err := s.execute(t.Context(), e, req)
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
	_, err := s.execute(t.Context(), e, tools.ShellRequest{ComputeID: "compute", Script: "true"})
	if core.CodeOf(err) != core.RateLimited {
		t.Fatal(err)
	}
	foreign := e.Principal
	foreign.SubjectID = "another"
	_, err = s.manage(t.Context(), foreign, e.AssistantID, tools.ShellTaskRequest{Action: "stop_all"})
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
	if (access{b.store}).liveAssistant(t.Context(), e.Principal) == nil {
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
	raw, err := s.execute(call, e, tools.ShellRequest{ComputeID: "compute", Script: "sleep 60"})
	need(t, err)
	var receipt struct {
		Task   shellTask
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
	if err := (access{b.store}).execution(run, e); err != nil {
		t.Fatal(err)
	}
	_, err = s.manage(t.Context(), e.Principal, e.AssistantID, tools.ShellTaskRequest{Action: "stop", TaskID: e.OperationID})
	need(t, err)
	s.wg.Wait()
	if managedShellFor(run, e) != nil {
		t.Fatal("completed capability alive")
	}
}
func TestShellRestartInterruptsWithoutReplay(t *testing.T) {
	b, p, e := taskFixture(t)
	s := b.shellTasks
	for _, mode := range []string{"foreground", "background", "tty"} {
		need(t, s.save(t.Context(), shellTask{ID: core.ID(mode), OwnerID: p.OwnerID, AssistantID: e.AssistantID, Mode: mode, State: "running", StartedAt: time.Now()}))
	}
	ctx, cancel := context.WithCancel(t.Context())
	need(t, s.start(ctx))
	cancel()
	s.wg.Wait()
	rows, err := persistence.Scan[shellTask](t.Context(), b.store, shellTasksBucket)
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
		_ = managedShellFor(ctx, e).started("", "", nil)
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return json.RawMessage(`{"Exit":{"Code":0}}`), nil
	}
	front := make(chan error, 1)
	go func() {
		_, err := s.execute(t.Context(), e, tools.ShellRequest{ComputeID: "compute", Script: "sleep 10"})
		front <- err
	}()
	<-started
	for i := 0; i < 9; i++ {
		q := e
		q.OperationID = core.ID(fmt.Sprintf("background%d", i))
		_, err := s.execute(t.Context(), q, tools.ShellRequest{ComputeID: "compute", Script: "sleep 10", Background: true})
		need(t, err)
		<-started
	}
	q := e
	q.OperationID = "overflow"
	_, err := s.execute(t.Context(), q, tools.ShellRequest{ComputeID: "compute", Script: "true", TTY: true})
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
