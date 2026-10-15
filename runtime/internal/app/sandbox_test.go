package app

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent/scheduler"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSandboxPlansRequireIndependentComputeStorageAndNetworkGrants(t *testing.T) {
	if platform.NativeSandbox() {
		t.Skip("requires Linux mount and network plans; native plans are covered by TestNativeSandboxGrantRoutingAndRevocation")
	}
	cfg := testConfig(t)
	cfg.Sandbox.Enabled = true
	cfg.Sandbox.RootFS = t.TempDir()
	cfg.Sandbox.WorkRoot = t.TempDir()
	cfg.Sandbox.MinFreeDiskBytes = 1
	cfg.Sandbox.CgroupRoot = "/sys/fs/cgroup/molr-test"
	directory := t.TempDir()
	cfg.Sandbox.Directories = []config.SandboxDirectory{{Path: directory}}
	cfg.Sandbox.Networks = []config.SandboxNetwork{{Name: "local-test", Protocols: []string{"tcp"}, Addresses: []string{"127.0.0.0/8", "::1/128"}, Ports: []string{"80", "8090-8099"}, ExcludeAddresses: []string{"127.0.0.2"}, ExcludePorts: []string{"8090-8092"}}}
	b, err := Build(t.Context(), cfg)
	need(t, err)
	defer b.Close()
	owner, _ := ownerSession(t, b)
	ctx := t.Context()
	e := core.Execution{Principal: core.Principal{OwnerID: owner.OwnerID, SubjectID: "sandbox-assistant", Kind: core.ActorAssistant, ExecutionID: "sandbox-run"}, AssistantID: "sandbox-assistant", OperationID: "shell"}
	need(t, b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "business.assistants", persistence.Key(owner.OwnerID, e.AssistantID), assistant.Assistant{ID: e.AssistantID, OwnerID: owner.OwnerID, State: assistant.Active, Version: 1}); err != nil {
			return err
		}
		return tx.Put(ctx, "agent.jobs", persistence.Key(owner.OwnerID, "sandbox-job"), scheduler.Job{State: scheduler.Running, ExecutionID: e.Principal.ExecutionID, Event: event.AssistantEvent{ID: "sandbox-job", OwnerID: owner.OwnerID, AssistantID: e.AssistantID}})
	}))
	connect := func(kind resource.Kind, provider, name string, binding resource.Binding) resource.Resource {
		t.Helper()
		r, err := b.Services.Resources.Connect(ctx, owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: name}, Kind: kind, Provider: provider, Name: name, Binding: binding})
		need(t, err)
		return r
	}
	compute := connect(resource.Compute, "local-sandbox", "compute", resource.Binding{})
	storage := connect(resource.Storage, "local-directory", "files", resource.Binding{RootPath: directory})
	network := connect(resource.Network, "sandbox-network", "network", resource.Binding{SandboxTarget: "local-test"})
	grant := func(id core.ID, key string, actions []policy.Action, scope policy.Scope) policy.Grant {
		t.Helper()
		r, err := resource.NewRepository(b.store).Get(ctx, owner.OwnerID, id)
		need(t, err)
		g, err := b.Services.Grants.Grant(ctx, owner, policy.GrantRequest{Constraints: testResourceConstraints(r.Kind), Mutation: core.Mutation{IdempotencyKey: key}, AssistantID: e.AssistantID, ResourceID: id, Actions: actions, Scope: scope})
		need(t, err)
		return g
	}
	request := tools.ShellRequest{ComputeID: compute.ID, Script: "echo hello"}
	if _, err := b.sandbox.plan(ctx, e, request); err == nil {
		t.Fatal("shell accepted without compute grant")
	}
	grant(compute.ID, "compute-grant", []policy.Action{policy.Execute}, policy.Scope{})
	_, err = b.sandbox.plan(ctx, e, request)
	need(t, err)
	request.Files = []tools.ShellFile{{ResourceID: storage.ID, Path: "/approved", Write: true}}
	grant(storage.ID, "write-only", []policy.Action{policy.Write}, policy.Scope{PathPrefixes: []string{"/approved"}})
	if _, err := b.sandbox.plan(ctx, e, request); err == nil {
		t.Fatal("write-only grant became readable mount")
	}
	readGrant := grant(storage.ID, "read-grant", []policy.Action{policy.Read}, policy.Scope{PathPrefixes: []string{"/approved"}})
	plan, err := b.sandbox.plan(ctx, e, request)
	need(t, err)
	if plan.Mounts[0].Source != filepath.Join(directory, "approved") || plan.Mounts[0].ReadOnly || plan.Mounts[0].Destination != resourceShellPath(storage.ID, "/approved") {
		t.Fatalf("wrong mount: %+v", plan.Mounts)
	}
	request.Files[0].Path = "/other"
	if _, err := b.sandbox.plan(ctx, e, request); err == nil {
		t.Fatal("path scope expanded")
	}
	request.Files[0].Path = "/approved"
	request.NetworkIDs = []core.ID{network.ID}
	if _, err := b.sandbox.plan(ctx, e, request); err == nil {
		t.Fatal("compute grant implied network access")
	}
	grant(network.ID, "network-grant", []policy.Action{policy.Execute}, policy.Scope{})
	plan, err = b.sandbox.plan(ctx, e, request)
	need(t, err)
	if len(plan.NetworkTargets) != 1 || !reflect.DeepEqual(plan.NetworkTargets[0], cfg.Sandbox.Networks[0].Target()) {
		t.Fatal("network target not deployment bound")
	}
	need(t, b.Services.Grants.Revoke(ctx, owner, policy.RevokeRequest{Mutation: core.Mutation{IdempotencyKey: "revoke", ExpectedVersion: readGrant.Version}, GrantID: readGrant.ID}))
	if _, err := b.sandbox.plan(ctx, e, request); err == nil {
		t.Fatal("revoked read remained usable")
	}
}
func TestSandboxDiscoveryIsOwnerScopedAndDoesNotExposeInternalPaths(t *testing.T) {
	b, err := Build(t.Context(), testConfig(t))
	need(t, err)
	defer b.Close()
	_, token := ownerSession(t, b)
	request := httptest.NewRequest("GET", "https://runtime.example/api/sandbox", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("Cookie", "__Host-molr_session="+token.Reveal())
	response := httptest.NewRecorder()
	b.HTTP.Handler().ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	var info struct {
		Enabled      bool
		Networks     []any
		Capabilities struct {
			Reason string
			Shell  bool
		}
	}
	need(t, json.Unmarshal(response.Body.Bytes(), &info))
	if !info.Enabled || info.Networks == nil || info.Capabilities.Shell || info.Capabilities.Reason == "sandbox_disabled" || info.Capabilities.Reason == "" {
		t.Fatalf("%+v", info)
	}
	request.Header.Del("Cookie")
	response = httptest.NewRecorder()
	b.HTTP.Handler().ServeHTTP(response, request)
	if response.Code == 200 {
		t.Fatal("anonymous sandbox configuration")
	}
}
