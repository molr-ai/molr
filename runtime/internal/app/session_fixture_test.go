package app

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/browser"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/shellsession"
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

const shellTasksBucket = "sandbox.shell_tasks"

func sessionRows(t *testing.T, s *shellsession.Manager, p core.Principal, id core.ID) []shellsession.Task {
	t.Helper()
	raw, err := s.Manage(t.Context(), p, id, tools.ShellTaskRequest{Action: "list"})
	need(t, err)
	var result struct{ Items []shellsession.Task }
	need(t, json.Unmarshal(raw, &result))
	return result.Items
}
func runningSession(t *testing.T, s *shellsession.Manager, p core.Principal, id, task core.ID) *shellsession.Task {
	t.Helper()
	for _, row := range sessionRows(t, s, p, id) {
		if row.ID == task && row.State == "running" {
			return &row
		}
	}
	return nil
}
func browserTask(t *testing.T, s *shellsession.Manager, p core.Principal, id core.ID) *shellsession.Task {
	t.Helper()
	raw, err := s.BrowserManage(t.Context(), p, id, tools.BrowserRequest{Action: browser.Action{Action: "status"}})
	need(t, err)
	var result struct{ Task *shellsession.Task }
	need(t, json.Unmarshal(raw, &result))
	return result.Task
}
func awaitSessionEnd(t *testing.T, s *shellsession.Manager, p core.Principal, id, task core.ID, timeout time.Duration) shellsession.Task {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		for _, row := range sessionRows(t, s, p, id) {
			if row.ID == task && row.State != "running" {
				return row
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("session did not finish", task)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func browserAction(ctx context.Context, s *shellsession.Manager, p core.Principal, id core.ID, q tools.BrowserRequest) (json.RawMessage, error) {
	if p.Kind == core.ActorOwner {
		return s.BrowserManage(ctx, p, id, q)
	}
	return s.BrowserTool(ctx, core.Execution{Principal: p, AssistantID: id, OperationID: "browser-integration-action"}, "browser_action", q)
}
func testForwardManager(b *Bundle, http config.HTTP, forward config.PortForward) *shellsession.Manager {
	b.shellTasks.Close()
	b.shellTasks = shellsession.New(b.store, shellTaskEnvironment{b.remote}, shellsession.Options{Identity: b.Services.Auth, ForwardHTTP: http, Forward: forward})
	return b.shellTasks
}

type observingSessionHost struct {
	shellTaskEnvironment
	entered chan context.Context
}

func (h observingSessionHost) Execute(ctx context.Context, e core.Execution, q tools.ShellRequest) (json.RawMessage, error) {
	if err := shellsession.ManagedFor(ctx, e).Started("", "", nil, nil); err != nil {
		return nil, err
	}
	h.entered <- ctx
	<-ctx.Done()
	return json.RawMessage(`{"Exit":{"Code":125,"Reason":"canceled"}}`), nil
}
func TestSessionHostPreservesExecutionCapability(t *testing.T) {
	b, p, e := taskFixture(t)
	host := observingSessionHost{shellTaskEnvironment{b.remote}, make(chan context.Context, 1)}
	manager := shellsession.New(b.store, host, shellsession.Options{})
	defer manager.Close()
	_, err := manager.Execute(t.Context(), e, tools.ShellRequest{ComputeID: "compute", Script: "wait", Background: true})
	need(t, err)
	managed := <-host.entered
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Delete(ctx, "agent.jobs", persistence.Key(p.OwnerID, "shell-job"))
	}))
	if host.CheckExecution(t.Context(), e) == nil {
		t.Fatal("ended execution remains authorized without managed context")
	}
	need(t, host.CheckExecution(managed, e))
	if !shellsession.ManagedPrincipal(managed, e.Principal) {
		t.Fatal("managed identity lost")
	}
	forged := e
	forged.Principal.ExecutionID = "another"
	if host.CheckExecution(managed, forged) == nil {
		t.Fatal("capability authorized a different execution")
	}
	manager.Close()
	if shellsession.ManagedFor(managed, e) != nil || shellsession.ManagedPrincipal(managed, e.Principal) {
		t.Fatal("released capability remains valid")
	}
}
