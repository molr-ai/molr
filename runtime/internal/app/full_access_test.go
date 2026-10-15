//go:build (linux && (amd64 || arm64)) || darwin

package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/prompt"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	sm "molr.ai/runtime/internal/sandboxmanager"
)

func directConfig(c *config.Config) {
	c.Sandbox.AllowFullAccess = true
	c.Sandbox.Bwrap = "/missing/bwrap"
	c.Sandbox.CgroupRoot = "/missing/cgroup"
	c.Sandbox.RootFS = "/missing/rootfs"
	c.Sandbox.MemoryBytes = 512 << 20
	c.Sandbox.Processes = 64
	c.Sandbox.MaxFileBytes = 1
	c.Sandbox.MinFreeDiskBytes = 1 << 60
	c.Sandbox.WorkspaceProjectQuotas = false
}

func TestFullAccessThroughToolExecutor(t *testing.T) {
	f := newResourceAccessFixture(t, func(c *config.Config, _ string) { directConfig(c) })
	grants, err := persistence.Scan[policy.Grant](t.Context(), f.b.store, "security.grants")
	need(t, err)
	for _, g := range grants {
		need(t, f.b.Services.Grants.Revoke(t.Context(), f.owner, policy.RevokeRequest{GrantID: g.ID, Mutation: core.Mutation{IdempotencyKey: "revoke-" + string(g.ID), ExpectedVersion: g.Version}}))
	}
	full, err := f.b.Services.Grants.Grant(t.Context(), f.owner, policy.GrantRequest{AssistantID: f.execution.AssistantID, ResourceID: f.compute.ID, Actions: []policy.Action{policy.Execute}, Scope: policy.Scope{FullAccess: true}, Mutation: core.Mutation{IdempotencyKey: "tool-full"}})
	need(t, err)
	executor := tools.NewExecutor(f.b.store, tools.Options{Authorizer: f.ad.policy, Operations: operation.NewService(f.b.store, operation.Options{Authorizer: f.ad.policy}), ShellExecute: f.b.shellTasks.execute})
	call := tools.Call{Execution: f.execution, Name: "shell_execute", Arguments: json.RawMessage(fmt.Sprintf(`{"compute_id":%q,"script":"printf native-tool-ok","full_access":true}`, f.compute.ID))}
	result, err := executor.Execute(t.Context(), call)
	need(t, err)
	if !strings.Contains(string(result.Content), "native-tool-ok") {
		t.Fatal(string(result.Content))
	}
	call.Execution.OperationID = "ordinary-tool"
	call.Arguments = json.RawMessage(fmt.Sprintf(`{"compute_id":%q,"script":"printf should-not-run"}`, f.compute.ID))
	if _, err := executor.Execute(t.Context(), call); core.CodeOf(err) != core.Forbidden {
		t.Fatal("Full Access must not authorize ordinary calls", err)
	}
	need(t, f.b.Services.Grants.Revoke(t.Context(), f.owner, policy.RevokeRequest{GrantID: full.ID, Mutation: core.Mutation{IdempotencyKey: "revoke-tool-full", ExpectedVersion: full.Version}}))
	call.Execution.OperationID = "revoked-tool"
	call.Arguments = json.RawMessage(fmt.Sprintf(`{"compute_id":%q,"script":"printf should-not-run","full_access":true}`, f.compute.ID))
	if _, err := executor.Execute(t.Context(), call); core.CodeOf(err) != core.Forbidden {
		t.Fatal("revoked Full Access executed", err)
	}
}
func TestFullAccessLocalAndRemote(t *testing.T) {
	for _, remote := range []bool{false, true} {
		name := "local"
		if remote {
			name = "remote"
		}
		t.Run(name, func(t *testing.T) {
			if remote && runtime.GOOS != "linux" {
				t.Skip("requires an in-process Linux remote server; covered by Linux CI")
			}
			f := newResourceAccessFixture(t, func(c *config.Config, _ string) { directConfig(c) })
			router := f.b.shellTasks.router
			files := &resourceFileTools{adapters: f.ad, router: router}
			router.files = files
			id := core.ID("local")
			compute := f.compute
			if remote {
				cfg := testConfig(t)
				directConfig(&cfg)
				backend, err := newSandboxBackend(t.Context(), cfg, quotaTestStore(t))
				need(t, err)
				store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "remote.db"))
				need(t, err)
				t.Cleanup(func() { store.Close() })
				manager, err := sm.New(t.Context(), store, backend)
				need(t, err)
				t.Cleanup(func() { manager.Close() })
				token := strings.Repeat("direct-token", 8)
				handler, err := sm.Handler(manager, core.NewSecret(token))
				need(t, err)
				server := httptest.NewTLSServer(handler)
				t.Cleanup(server.Close)
				sum := sha256.Sum256(server.Certificate().Raw)
				router.adapters.networkConfig.AllowedPrivateIPs = []string{"127.0.0.1"}
				input, _ := json.Marshal(map[string]any{"Name": "Full Access remote", "Endpoint": server.URL, "Fingerprint": hex.EncodeToString(sum[:]), "Token": token, "Mutation": core.Mutation{IdempotencyKey: "remote-connect"}})
				out, err := router.admin(t.Context(), f.owner, "connect", input)
				need(t, err)
				id = out.(sandboxView).ID
				input, _ = json.Marshal(map[string]any{"SandboxID": id, "Kind": "compute", "SourceID": "compute", "Name": "Full Access", "Mutation": core.Mutation{IdempotencyKey: "remote-compute"}})
				out, err = router.admin(t.Context(), f.owner, "resource", input)
				need(t, err)
				compute = out.(resource.Resource)
			}
			// Remove fixture grants: node execution permission is required even for files.
			grants, err := persistence.Scan[policy.Grant](t.Context(), f.b.store, "security.grants")
			need(t, err)
			for _, g := range grants {
				need(t, f.b.Services.Grants.Revoke(t.Context(), f.owner, policy.RevokeRequest{GrantID: g.ID, Mutation: core.Mutation{IdempotencyKey: "revoke-" + string(g.ID), ExpectedVersion: g.Version}}))
			}
			external := filepath.Join(t.TempDir(), "outside.txt")
			target := tools.FileTarget{FullAccess: true, SandboxID: id, Path: external}
			if _, err := files.write(t.Context(), f.execution, tools.FileWriteRequest{FileTarget: target, Text: "outside"}); err == nil {
				t.Fatal("file access without node grant")
			}
			grant, err := f.b.Services.Grants.Grant(t.Context(), f.owner, policy.GrantRequest{AssistantID: f.execution.AssistantID, ResourceID: compute.ID, Actions: []policy.Action{policy.Execute}, Scope: policy.Scope{FullAccess: true}, Mutation: core.Mutation{IdempotencyKey: "full-access"}})
			need(t, err)
			if grant.Constraints != nil {
				t.Fatal("full access requires no quota")
			}
			_, err = files.write(t.Context(), f.execution, tools.FileWriteRequest{FileTarget: target, Text: "outside"})
			need(t, err)
			data, err := os.ReadFile(external)
			need(t, err)
			if string(data) != "outside" {
				t.Fatal(string(data))
			}
			hard := external + "-hard"
			need(t, os.Link(external, hard))
			hardRead := f.execution
			hardRead.OperationID = "read-hard"
			if _, err := files.read(t.Context(), hardRead, tools.FileReadRequest{FileTarget: tools.FileTarget{FullAccess: true, SandboxID: id, Path: hard}}); err != nil {
				t.Fatal(err)
			}
			// Symbolic and hard links follow the operating system's full-access semantics.
			linked := external + "-link"
			need(t, os.Symlink(external, linked))
			read := f.execution
			read.OperationID = "read-link"
			raw, err := files.read(t.Context(), read, tools.FileReadRequest{FileTarget: tools.FileTarget{FullAccess: true, SandboxID: id, Path: linked}})
			need(t, err)
			if !strings.Contains(string(raw), "outside") {
				t.Fatal(string(raw))
			}
			info, err := router.info(t.Context(), f.owner, id)
			need(t, err)
			if !info.Capabilities.FullAccess || info.Capabilities.Shell {
				t.Fatalf("false capabilities: %+v", info)
			}
			network := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("network-ok")) }))
			defer network.Close()
			run := f.execution
			run.OperationID = "direct-command"
			raw, err = router.shellExecute(t.Context(), run, tools.ShellRequest{SandboxID: id, FullAccess: true, ComputeID: compute.ID, TimeoutSeconds: 10, Script: "cat '" + external + "'; curl -fsS --noproxy '*' '" + network.URL + "'; printf '\\n'; pwd; printf '%01000d' 0"})
			need(t, err)
			var result struct {
				Exit           platform.Exit
				Stdout         string
				OutputComplete bool
			}
			need(t, json.Unmarshal(raw, &result))
			if result.Exit.Code != 0 || !strings.Contains(result.Stdout, "outsidenetwork-ok") || !result.OutputComplete {
				t.Fatal(string(raw))
			}
			// Direct mode must not claim it honored a request for network isolation.
			run.OperationID = "no-network"
			_, err = router.shellExecute(t.Context(), run, tools.ShellRequest{SandboxID: id, FullAccess: true, ComputeID: compute.ID, Script: "true", NetworkMode: "none"})
			if err == nil {
				t.Fatal("network isolation silently ignored")
			}
			need(t, f.b.Services.Grants.Revoke(t.Context(), f.owner, policy.RevokeRequest{GrantID: grant.ID, Mutation: core.Mutation{IdempotencyKey: "revoke-full", ExpectedVersion: grant.Version}}))
			run.OperationID = "denied"
			if _, err = router.shellExecute(t.Context(), run, tools.ShellRequest{SandboxID: id, FullAccess: true, ComputeID: compute.ID, Script: "true"}); err == nil {
				t.Fatal("execution survived grant revocation")
			}
		})
	}
}

func TestFullAccessConfigAndDisabled(t *testing.T) {
	cfg := testConfig(t)
	directConfig(&cfg)
	need(t, cfg.Validate())
	cfg.Sandbox.AllowFullAccess = true
	cfg.Sandbox.Enabled = false
	b, err := newSandboxBackend(t.Context(), cfg, quotaTestStore(t))
	need(t, err)
	defer b.Close()
	info, err := b.Info(t.Context())
	need(t, err)
	if info.Enabled || info.Storage || info.Capabilities.Shell || info.Capabilities.FullAccess {
		t.Fatal(info)
	}
	_, err = b.Call(t.Context(), sm.Request{Identity: sm.Identity{OwnerID: "owner", AssistantID: "assistant", ExecutionID: "run", OperationID: "op"}, Method: "shell_execute", Compute: true, Deadline: time.Now().Add(time.Minute), Arguments: json.RawMessage(`{"script":"true"}`)})
	if err == nil {
		t.Fatal("disabled backend executed")
	}
}

func TestFullAccessBackgroundTasksHaveNoTenTaskQuota(t *testing.T) {
	f := newResourceAccessFixture(t, func(c *config.Config, _ string) { directConfig(c) })
	_, err := f.b.Services.Grants.Grant(t.Context(), f.owner, policy.GrantRequest{AssistantID: f.execution.AssistantID, ResourceID: f.compute.ID, Actions: []policy.Action{policy.Execute}, Scope: policy.Scope{FullAccess: true}, Mutation: core.Mutation{IdempotencyKey: "full-background"}})
	need(t, err)
	for i := 0; i < 11; i++ {
		e := f.execution
		e.OperationID = core.ID(fmt.Sprintf("direct-background-%d", i))
		_, err := f.b.shellTasks.execute(t.Context(), e, tools.ShellRequest{FullAccess: true, ComputeID: f.compute.ID, Script: "sleep 60", Background: true})
		need(t, err)
	}
	f.b.shellTasks.mu.Lock()
	count := len(f.b.shellTasks.active)
	f.b.shellTasks.mu.Unlock()
	if count != 11 {
		t.Fatalf("expected 11 running direct tasks, got %d", count)
	}
}

func TestFullAccessPermissionIsExplicit(t *testing.T) {
	f := newResourceAccessFixture(t, func(c *config.Config, _ string) { directConfig(c) })
	router := f.b.shellTasks.router
	files := &resourceFileTools{adapters: f.ad, router: router}
	e := f.execution
	outside := filepath.Join(t.TempDir(), "private")
	need(t, os.WriteFile(outside, []byte("private"), 0600))
	request := tools.ShellRequest{FullAccess: true, ComputeID: f.compute.ID, Script: "printf full"}
	if _, err := router.shellExecute(t.Context(), e, request); core.CodeOf(err) != core.Forbidden {
		t.Fatal("ordinary grant elevated", err)
	}
	if _, err := files.read(t.Context(), e, tools.FileReadRequest{FileTarget: tools.FileTarget{FullAccess: true, Path: outside}}); core.CodeOf(err) != core.Forbidden {
		t.Fatal("ordinary grant elevated files", err)
	}
	full, err := f.b.Services.Grants.Grant(t.Context(), f.owner, policy.GrantRequest{AssistantID: e.AssistantID, ResourceID: f.compute.ID, Actions: []policy.Action{policy.Execute}, Scope: policy.Scope{FullAccess: true}, Mutation: core.Mutation{IdempotencyKey: "full"}})
	need(t, err)
	if _, err := files.read(t.Context(), e, tools.FileReadRequest{FileTarget: tools.FileTarget{Path: outside}}); err == nil {
		t.Fatal("file call upgraded without explicit flag")
	}
	if _, err := files.read(t.Context(), e, tools.FileReadRequest{FileTarget: tools.FileTarget{FullAccess: true, Path: outside}}); err != nil {
		t.Fatal(err)
	}
	raw, err := router.list(t.Context(), e.Principal)
	need(t, err)
	projected, err := prompt.Structured("permission_list", string(raw))
	need(t, err)
	if !strings.Contains(projected, `"FullAccessAvailable":true`) || !strings.Contains(projected, `"FullAccessComputeIDs"`) || !strings.Contains(projected, `"FullAccess":true`) {
		t.Fatal("Full Access stripped from model view", projected)
	}
	if !strings.Contains(string(raw), `"FullAccessAvailable":true`) || !strings.Contains(string(raw), `"FullAccessComputeIDs"`) {
		t.Fatal(string(raw))
	}
	bad := policy.GrantRequest{AssistantID: e.AssistantID, ResourceID: f.compute.ID, Actions: []policy.Action{policy.Execute}, Scope: policy.Scope{FullAccess: true}, Constraints: testResourceConstraints(resource.Compute), Mutation: core.Mutation{IdempotencyKey: "full-quota"}}
	if _, err := f.b.Services.Grants.Grant(t.Context(), f.owner, bad); err == nil {
		t.Fatal("Full Access accepted quotas")
	}
	_, err = f.b.Services.Grants.UpdateGrant(t.Context(), f.owner, policy.UpdateGrantRequest{GrantID: full.ID, Actions: full.Actions, Constraints: testResourceConstraints(resource.Compute), Mutation: core.Mutation{IdempotencyKey: "duplicate-isolated", ExpectedVersion: full.Version}})
	if core.CodeOf(err) != core.Conflict {
		t.Fatal("stacked ordinary quota through Full Access edit", err)
	}
	e.OperationID = "normal-no-fallback"
	if _, err := router.shellExecute(t.Context(), e, tools.ShellRequest{ComputeID: f.compute.ID, Script: "printf should-not-run"}); err == nil {
		t.Fatal("ordinary call fell back to full access")
	}
	need(t, f.b.Services.Grants.Revoke(t.Context(), f.owner, policy.RevokeRequest{GrantID: full.ID, Mutation: core.Mutation{IdempotencyKey: "revoke-full-explicit", ExpectedVersion: full.Version}}))
	e.OperationID = "revoked"
	if _, err := router.shellExecute(t.Context(), e, request); core.CodeOf(err) != core.Forbidden {
		t.Fatal("revoked full access still works", err)
	}
}

func TestFullAccessAndIsolationCoexist(t *testing.T) {
	if os.Getenv("MOLR_SANDBOX_INTEGRATION") != "1" {
		t.Skip("requires delegated isolation integration service")
	}
	exe, err := os.Executable()
	need(t, err)
	need(t, os.Chmod(exe, 0755))
	f := newResourceAccessFixture(t, func(c *config.Config, _ string) {
		c.Sandbox.AllowFullAccess = true
		c.Sandbox.CgroupRoot = os.Getenv("MOLR_SANDBOX_CGROUP")
		c.Sandbox.WorkRoot = t.TempDir()
		c.Sandbox.MinFreeDiskBytes = 1
	})
	if err := f.b.sandbox.runner.Prepare(t.Context()); err != nil {
		t.Fatalf("prepare: %v reason=%s", err, core.ReasonOf(err))
	}
	caps, err := f.b.sandbox.runner.Capabilities(t.Context())
	need(t, err)
	if !caps.Shell || !caps.FullAccess || !caps.FileIsolation {
		t.Fatal(caps)
	}
	full, err := f.b.Services.Grants.Grant(t.Context(), f.owner, policy.GrantRequest{AssistantID: f.execution.AssistantID, ResourceID: f.compute.ID, Actions: []policy.Action{policy.Execute}, Scope: policy.Scope{FullAccess: true}, Mutation: core.Mutation{IdempotencyKey: "full-coexist"}})
	need(t, err)
	outside := filepath.Join(t.TempDir(), "private")
	need(t, os.WriteFile(outside, []byte("full-ok"), 0600))
	for _, isFull := range []bool{false, true} {
		e := f.execution
		e.OperationID = core.ID(fmt.Sprintf("coexist-%t", isFull))
		script := "test ! -e '" + outside + "' && printf isolated-ok"
		if isFull {
			script = "cat '" + outside + "'"
		}
		raw, err := f.b.sandbox.execute(t.Context(), e, tools.ShellRequest{FullAccess: isFull, ComputeID: f.compute.ID, Script: script})
		need(t, err)
		var out struct {
			Exit   platform.Exit
			Stdout string
		}
		need(t, json.Unmarshal(raw, &out))
		if out.Exit.Code != 0 {
			t.Fatal(string(raw))
		}
		expected := "isolated-ok"
		if isFull {
			expected = "full-ok"
		}
		if out.Stdout != expected {
			t.Fatal(string(raw))
		}
	}
	// Both run on the same runner. Revoking only Full Access must leave the
	// separately authorized isolated task running.
	for _, isFull := range []bool{false, true} {
		e := f.execution
		e.OperationID = core.ID(fmt.Sprintf("background-%t", isFull))
		_, err := f.b.shellTasks.execute(t.Context(), e, tools.ShellRequest{FullAccess: isFull, ComputeID: f.compute.ID, Script: "sleep 30", Background: true})
		need(t, err)
	}
	need(t, f.b.Services.Grants.Revoke(t.Context(), f.owner, policy.RevokeRequest{GrantID: full.ID, Mutation: core.Mutation{IdempotencyKey: "revoke-coexist", ExpectedVersion: full.Version}}))
	deadline := time.Now().Add(3 * time.Second)
	for {
		f.b.shellTasks.mu.Lock()
		normal := f.b.shellTasks.active[string(f.owner.OwnerID)+"/background-false"]
		fullTask := f.b.shellTasks.active[string(f.owner.OwnerID)+"/background-true"]
		f.b.shellTasks.mu.Unlock()
		if normal == nil {
			t.Fatal("isolated task stopped when full access revoked")
		}
		if fullTask == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("full access task not canceled")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestFullAccessDeploymentGateAndGrantExpiry(t *testing.T) {
	t.Run("deployment", func(t *testing.T) {
		f := newResourceAccessFixture(t, func(c *config.Config, _ string) { directConfig(c); c.Sandbox.AllowFullAccess = false })
		_, err := f.b.Services.Grants.Grant(t.Context(), f.owner, policy.GrantRequest{AssistantID: f.execution.AssistantID, ResourceID: f.compute.ID, Actions: []policy.Action{policy.Execute}, Scope: policy.Scope{FullAccess: true}, Mutation: core.Mutation{IdempotencyKey: "disabled-full"}})
		if err == nil {
			t.Fatal("Full Access granted without deployment permission")
		}
		if caps, _ := f.b.sandbox.runner.Capabilities(t.Context()); caps.FullAccess {
			t.Fatal(caps)
		}
	})
	t.Run("expiry", func(t *testing.T) {
		f := newResourceAccessFixture(t, func(c *config.Config, _ string) { directConfig(c) })
		until := time.Now().Add(time.Second)
		_, err := f.b.Services.Grants.Grant(t.Context(), f.owner, policy.GrantRequest{AssistantID: f.execution.AssistantID, ResourceID: f.compute.ID, Actions: []policy.Action{policy.Execute}, Scope: policy.Scope{FullAccess: true}, ExpiresAt: &until, Mutation: core.Mutation{IdempotencyKey: "expires-full"}})
		need(t, err)
		e := f.execution
		e.OperationID = "expires-call"
		raw, err := f.b.sandbox.execute(t.Context(), e, tools.ShellRequest{FullAccess: true, ComputeID: f.compute.ID, Script: "sleep 30"})
		need(t, err)
		var out struct{ Exit platform.Exit }
		need(t, json.Unmarshal(raw, &out))
		if out.Exit.Code == 0 {
			t.Fatal("expired full-access command kept running")
		}
		e.OperationID = "after-expiry"
		if _, err := f.b.sandbox.execute(t.Context(), e, tools.ShellRequest{FullAccess: true, ComputeID: f.compute.ID, Script: "printf should-not-run"}); core.CodeOf(err) != core.Forbidden {
			t.Fatal(err)
		}
		if _, err := f.b.sandbox.plan(t.Context(), e, tools.ShellRequest{ComputeID: f.compute.ID, Script: "true"}); err != nil {
			t.Fatal("ordinary permission lost after full-access expiry", err)
		}
	})
}

func TestFullAccessRequiresGroupDelegation(t *testing.T) {
	f := newMultiUserFixture(t, directConfig)
	ctx := t.Context()
	router := f.b.shellTasks.router
	r, err := f.b.Services.Resources.Connect(ctx, f.admin, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "compute"}, Kind: resource.Compute, Provider: "local-sandbox", Name: "Compute"})
	need(t, err)
	f.group.Resources = append(f.group.Resources, auth.ResourceReference{Kind: "tool", ID: "permission_list"}, auth.ResourceReference{Kind: "tool", ID: "shell_execute"}, auth.ResourceReference{Kind: "sandbox", ID: "local", Quota: auth.ResourceQuota{Concurrent: 1}})
	save := func(key string) {
		var err error
		f.group, err = f.b.Services.Auth.(*auth.Manager).SaveGroup(ctx, f.admin, auth.SaveGroupRequest{Group: f.group, Mutation: core.Mutation{IdempotencyKey: key, ExpectedVersion: f.group.Version}})
		need(t, err)
	}
	save("node")
	a := f.assistant(t, f.alice, "full-access-member")
	q := policy.GrantRequest{AssistantID: a.ID, ResourceID: r.ID, Actions: []policy.Action{policy.Execute}, Scope: policy.Scope{FullAccess: true}, Mutation: core.Mutation{IdempotencyKey: "full-member"}}
	if _, err := f.b.Services.Grants.Grant(ctx, f.alice, q); core.CodeOf(err) != core.Forbidden {
		t.Fatal("member self-elevated", err)
	}
	f.group.Resources[len(f.group.Resources)-1].AllowFullAccess = true
	save("allow-full")
	_, err = f.b.Services.Grants.Grant(ctx, f.alice, q)
	need(t, err)
	e := f.execution(t, f.alice, a, "full-member")
	need(t, router.requireFullAccess(ctx, e, "local"))
	_, err = f.b.Services.Grants.Grant(ctx, f.alice, policy.GrantRequest{AssistantID: a.ID, ResourceID: r.ID, Actions: []policy.Action{policy.Execute}, Constraints: testResourceConstraints(resource.Compute), Mutation: core.Mutation{IdempotencyKey: "ordinary-member"}})
	need(t, err)
	for i := 0; i < 2; i++ {
		run := e
		run.OperationID = core.ID(fmt.Sprintf("full-member-%d", i))
		_, err = f.b.shellTasks.execute(ctx, run, tools.ShellRequest{FullAccess: true, ComputeID: r.ID, Script: "sleep 30", Background: true})
		need(t, err)
	}
	ordinary := e
	ordinary.OperationID = "ordinary-member"
	_, err = f.b.shellTasks.execute(ctx, ordinary, tools.ShellRequest{ComputeID: r.ID, Script: "true"})
	if core.ReasonOf(err) == "group_quota_exceeded" {
		t.Fatal("Full Access consumed ordinary group slots", err)
	}
	if err == nil {
		t.Fatal("missing isolation unexpectedly available")
	}
	f.group.Resources[len(f.group.Resources)-1].AllowFullAccess = false
	save("deny-full")
	if err := router.requireFullAccess(ctx, e, "local"); core.CodeOf(err) != core.Forbidden {
		t.Fatal("removed delegation still active", err)
	}
	raw, err := router.list(ctx, e.Principal)
	need(t, err)
	var listing resourceListing
	need(t, json.Unmarshal(raw, &listing))
	for _, v := range listing.Requestable {
		if v.FullAccessAvailable {
			t.Fatal("group choice leaked")
		}
	}
	for _, v := range listing.Resources {
		if v.FullAccess {
			t.Fatal("group authorization leaked")
		}
	}
}
