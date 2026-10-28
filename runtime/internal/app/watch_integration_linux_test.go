//go:build linux && (amd64 || arm64)

package app

import (
	"context"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/business/watch"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"os"
	"testing"
	"time"
)

func watcherIntegration(t *testing.T, script string, expiry *time.Time) (*Bundle, core.Principal, watch.Watch) {
	t.Helper()
	if os.Getenv("MOLR_SANDBOX_INTEGRATION") != "1" {
		t.Skip("real sandbox required")
	}
	exe, err := os.Executable()
	need(t, err)
	need(t, os.Chmod(exe, 0755))
	b, p, e := taskFixture(t)
	need(t, b.sandbox.runner.Prepare(t.Context()))
	compute, err := b.Services.Resources.Connect(t.Context(), p, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "watch-compute"}, Kind: resource.Compute, Provider: "local-sandbox", Name: "Watcher compute"})
	need(t, err)
	_, err = b.Services.Grants.Grant(t.Context(), p, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "watch-grant"}, AssistantID: e.AssistantID, ResourceID: compute.ID, Actions: []policy.Action{policy.Execute}, ExpiresAt: expiry, Constraints: testResourceConstraints(resource.Compute)})
	need(t, err)
	w, err := b.Services.Watches.Save(t.Context(), p, watch.Request{Mutation: core.Mutation{IdempotencyKey: "watch-plan"}, AssistantID: e.AssistantID, Title: "Integration watch", Instructions: "Report the observed value", Watcher: watch.Config{ComputeID: compute.ID, Script: script}})
	need(t, err)
	return b, p, w
}

const watcherHandshake = `read start
printf '%s\n' '{"type":"ready","version":1}'
read ack
`

func TestWatcherRealProtocolAndCompletion(t *testing.T) {
	script := watcherHandshake + `printf '%s\n' '{"type":"checkpoint","state":{"cursor":1}}'
read ack
printf '%s\n' '{"type":"event","key":"change-1","data":{"value":42},"state":{"cursor":2}}'
read ack
printf '%s\n' '{"type":"complete"}'
read ack
`
	b, p, w := watcherIntegration(t, script, nil)
	next, err := b.Services.Watches.Begin(t.Context(), p.OwnerID, w.ID, w.Version)
	need(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	b.watchRuntime.run(ctx, next, "")
	result, err := b.Services.Watches.Get(t.Context(), p, w.ID)
	need(t, err)
	if result.State != "completed" || len(result.Pending) != 1 || string(result.Checkpoint) != `{"cursor":2}` {
		t.Fatalf("watcher protocol did not finish: %+v", result)
	}
	events, err := persistence.Select[event.AssistantEvent](t.Context(), b.store, "agent.events", persistence.Query{Where: []persistence.Filter{{Field: "Kind", Op: "in", Value: []event.Kind{event.ShellTaskFinished, event.ShellTasksRunning}}}})
	need(t, err)
	if len(events) != 0 {
		t.Fatalf("watcher leaked ordinary Shell notifications: %+v", events)
	}
}
func TestWatcherRealGrantExpiryBlocksAndPreservesCheckpoint(t *testing.T) {
	expiry := time.Now().Add(5 * time.Second)
	b, p, w := watcherIntegration(t, watcherHandshake+`printf '%s\n' '{"type":"checkpoint","state":{"cursor":7}}'
read ack
sleep 30
`, &expiry)
	next, err := b.Services.Watches.Begin(t.Context(), p.OwnerID, w.ID, w.Version)
	need(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	b.watchRuntime.run(ctx, next, "initial-grants")
	result, err := b.Services.Watches.Get(t.Context(), p, w.ID)
	need(t, err)
	if result.State != "blocked" || result.Reason != "authorization_unavailable" || string(result.Checkpoint) != `{"cursor":7}` {
		t.Fatalf("grant expiry: %+v", result)
	}
	events, err := persistence.Select[event.AssistantEvent](t.Context(), b.store, "agent.events", persistence.Query{Where: []persistence.Filter{persistence.Eq("Kind", event.WatchFailed)}})
	need(t, err)
	if len(events) != 1 {
		t.Fatalf("expected one failure event, got %d", len(events))
	}
}

func TestWatcherRealDispatchesWhileProcessRemainsRunning(t *testing.T) {
	b, p, w := watcherIntegration(t, watcherHandshake+`printf '%s\n' '{"type":"event","key":"live-change","data":{"value":42}}'
read ack
sleep 30
`, nil)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		need(t, b.watchRuntime.tick(t.Context(), time.Now()))
		tasks, err := persistence.Select[struct{ ID core.ID }](t.Context(), b.store, "business.tasks", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID), persistence.Eq("Plan.Kind", "watch"), persistence.Eq("Plan.ID", w.ID)}})
		need(t, err)
		if len(tasks) > 0 {
			current, err := b.Services.Watches.Get(t.Context(), p, w.ID)
			need(t, err)
			if len(tasks) != 1 || current.State != "running" {
				t.Fatalf("dispatch must not require watcher exit: %+v", current)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("live watcher did not dispatch its change")
}

func TestWatcherRealRemoteProtocol(t *testing.T) {
	if os.Getenv("MOLR_SANDBOX_INTEGRATION") != "1" {
		t.Skip("real sandbox required")
	}
	exe, err := os.Executable()
	need(t, err)
	need(t, os.Chmod(exe, 0755))
	b, p, e := taskFixture(t)
	compute := forwardCompute(t, b, p, true)
	remote, err := resource.NewRepository(b.store).Get(t.Context(), p.OwnerID, compute)
	need(t, err)
	_, err = b.Services.Grants.Grant(t.Context(), p, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "watch-remote-grant"}, AssistantID: e.AssistantID, ResourceID: compute, Actions: []policy.Action{policy.Execute}, Constraints: testResourceConstraints(resource.Compute)})
	need(t, err)
	w, err := b.Services.Watches.Save(t.Context(), p, watch.Request{Mutation: core.Mutation{IdempotencyKey: "watch-remote"}, AssistantID: e.AssistantID, Title: "Remote watch", Instructions: "Report changes", Watcher: watch.Config{ComputeID: compute, SandboxID: remote.SandboxID, Script: watcherHandshake + `printf '%s\n' '{"type":"event","key":"remote-one","data":{"value":42},"state":{"cursor":1}}'
read ack
printf '%s\n' '{"type":"complete"}'
read ack
`}})
	need(t, err)
	w, err = b.Services.Watches.Begin(t.Context(), p.OwnerID, w.ID, w.Version)
	need(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	b.watchRuntime.run(ctx, w, "")
	got, err := b.Services.Watches.Get(t.Context(), p, w.ID)
	need(t, err)
	if got.State != "completed" || len(got.Pending) != 1 || string(got.Checkpoint) != `{"cursor":1}` {
		t.Fatalf("remote protocol: %+v", got)
	}
}
