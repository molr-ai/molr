//go:build linux && (amd64 || arm64)

package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	sm "molr.ai/runtime/internal/sandboxmanager"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestShellTasksRealDetachedAuthorizationAndTTY(t *testing.T) {
	if os.Getenv("MOLR_SANDBOX_INTEGRATION") != "1" {
		t.Skip("real sandbox required")
	}
	exe, err := os.Executable()
	need(t, err)
	need(t, os.Chmod(exe, 0755))
	b, owner, e := taskFixture(t)
	s := b.shellTasks
	need(t, b.sandbox.runner.Prepare(t.Context()))
	compute, err := b.Services.Resources.Connect(t.Context(), owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "compute"}, Kind: resource.Compute, Provider: "local-sandbox", Name: "compute"})
	need(t, err)
	grant, err := b.Services.Grants.Grant(t.Context(), owner, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "grant"}, AssistantID: e.AssistantID, ResourceID: compute.ID, Actions: []policy.Action{policy.Execute}, Constraints: testResourceConstraints(resource.Compute)})
	need(t, err)
	e.OperationID = "terminal"
	raw, err := s.execute(t.Context(), e, tools.ShellRequest{ComputeID: compute.ID, TTY: true, Script: "exec /bin/sh -i"})
	need(t, err)
	if !strings.Contains(string(raw), "TTY") {
		t.Fatal(string(raw))
	}
	var terminal *runningShell
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		s.mu.Lock()
		terminal = s.active[shellKey(e)]
		ready := terminal != nil && terminal.scope.control != nil
		s.mu.Unlock()
		if ready {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.OperationID = "background"
	_, err = s.execute(t.Context(), e, tools.ShellRequest{ComputeID: compute.ID, Background: true, Script: "echo begin; sleep 2; echo detached_ok"})
	need(t, err)
	// A task keeps only its process-local capability after the original job ends.
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Delete(ctx, "agent.jobs", persistence.Key(owner.OwnerID, "shell-job"))
	}))
	_, err = s.manage(t.Context(), owner, e.AssistantID, tools.ShellTaskRequest{Action: "takeover", TaskID: "terminal", ClientID: "integration", Generation: 1})
	need(t, err)
	_, err = s.manage(t.Context(), owner, e.AssistantID, tools.ShellTaskRequest{Action: "write", TaskID: "terminal", ClientID: "integration", Generation: 2, Sequence: 1, Data: "echo terminal_survived\n"})
	need(t, err)
	time.Sleep(2300 * time.Millisecond)
	bg, err := persistence.Load[shellTask](t.Context(), b.store, shellTasksBucket, persistence.Key(owner.OwnerID, "background"))
	need(t, err)
	if bg.State != "completed" || !strings.Contains(string(bg.Result), "detached_ok") {
		t.Fatalf("detached failed: %+v", bg)
	}
	raw, err = s.manage(t.Context(), owner, e.AssistantID, tools.ShellTaskRequest{Action: "read", TaskID: "terminal"})
	need(t, err)
	if !strings.Contains(string(raw), "terminal_survived") {
		t.Fatal(string(raw))
	}
	// Revocation still kills a detached terminal after the loop is gone.
	need(t, b.Services.Grants.Revoke(t.Context(), owner, policy.RevokeRequest{Mutation: core.Mutation{IdempotencyKey: "revoke", ExpectedVersion: grant.Version}, GrantID: grant.ID}))
	select {
	case <-terminal.done:
	case <-time.After(5 * time.Second):
		t.Fatal("terminal survived grant revocation")
	}
	var result struct{ Exit struct{ Code int } }
	need(t, json.Unmarshal(terminal.result, &result))
	if result.Exit.Code == 0 {
		t.Fatal("revoked terminal exited successfully")
	}
}

func TestShellTasksRemoteTTYAndArchivedOutput(t *testing.T) {
	if os.Getenv("MOLR_SANDBOX_INTEGRATION") != "1" {
		t.Skip("real sandbox required")
	}
	exe, err := os.Executable()
	need(t, err)
	need(t, os.Chmod(exe, 0755))
	b, owner, e := taskFixture(t)
	cfg := testConfig(t)
	backend, err := newSandboxBackend(t.Context(), cfg, quotaTestStore(t))
	need(t, err)
	remoteStore, err := sqlitestore.Open(filepath.Join(t.TempDir(), "remote.db"))
	need(t, err)
	defer remoteStore.Close()
	manager, err := sm.New(t.Context(), remoteStore, backend)
	need(t, err)
	defer manager.Close()
	token := strings.Repeat("session-test-", 8)
	handler, err := sm.Handler(manager, core.NewSecret(token))
	need(t, err)
	server := httptest.NewTLSServer(handler)
	defer server.Close()
	fingerprint := sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)
	router := b.shellTasks.router
	raw, _ := json.Marshal(map[string]any{"Name": "Remote", "Endpoint": server.URL, "ConnectionCode": sandboxConnectionCode(hex.EncodeToString(fingerprint[:]), token), "Mutation": core.Mutation{IdempotencyKey: "connect"}})
	view, err := router.admin(t.Context(), owner, "connect", raw)
	need(t, err)
	id := view.(sandboxView).ID
	raw, _ = json.Marshal(map[string]any{"SandboxID": id, "Kind": "compute", "SourceID": "compute", "Name": "Remote compute", "Mutation": core.Mutation{IdempotencyKey: "compute"}})
	_, err = router.admin(t.Context(), owner, "resource", raw)
	need(t, err)
	resources, err := persistence.Scan[resource.Resource](t.Context(), b.store, "business.resources")
	need(t, err)
	var compute core.ID
	for _, r := range resources {
		if r.SandboxID == id && r.Kind == resource.Compute {
			compute = r.ID
		}
	}
	if compute == "" {
		t.Fatal("missing remote compute")
	}
	_, err = b.Services.Grants.Grant(t.Context(), owner, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "grant"}, AssistantID: e.AssistantID, ResourceID: compute, Actions: []policy.Action{policy.Execute}, Constraints: testResourceConstraints(resource.Compute)})
	need(t, err)
	_, err = b.shellTasks.execute(t.Context(), e, tools.ShellRequest{SandboxID: id, ComputeID: compute, TTY: true, Script: "exec /bin/sh -i"})
	need(t, err)
	deadline := time.Now().Add(8 * time.Second)
	for {
		_, err = b.shellTasks.manage(t.Context(), e.Principal, e.AssistantID, tools.ShellTaskRequest{Action: "write", TaskID: e.OperationID, Generation: 1, Data: "echo remote_tty_ok; exit\n"})
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(30 * time.Millisecond)
	}
	for {
		row, err := persistence.Load[shellTask](t.Context(), b.store, shellTasksBucket, persistence.Key(owner.OwnerID, e.OperationID))
		need(t, err)
		if row.State != "running" {
			if row.State != "completed" {
				t.Fatalf("remote task: %+v", row)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("remote shell did not finish")
		}
		time.Sleep(30 * time.Millisecond)
	}
	// Completed logs must remain readable from the local workspace while the remote node is offline.
	server.Close()
	raw, err = b.shellTasks.manage(t.Context(), owner, e.AssistantID, tools.ShellTaskRequest{Action: "read", TaskID: e.OperationID})
	need(t, err)
	if !strings.Contains(string(raw), "remote_tty_ok") {
		t.Fatal(string(raw))
	}
}
