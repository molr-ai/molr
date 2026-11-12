//go:build (linux && (amd64 || arm64)) || darwin

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform/hostprograms"
	"molr.ai/runtime/internal/policy"
)

func TestHostProgramsDiscoverAuthorizeExecuteAndRevoke(t *testing.T) {
	f := newResourceAccessFixture(t, func(c *config.Config, _ string) { directConfig(c) })
	dir := t.TempDir()
	executable := filepath.Join(dir, "codex")
	need(t, os.WriteFile(executable, []byte("#!/bin/sh\nprintf '%s' \"$1\"\n"), 0755))
	t.Setenv("PATH", dir)
	router := f.b.remote
	input := json.RawMessage(`{"ID":"local","Command":"codex","Refresh":true}`)
	result, err := router.admin(t.Context(), f.owner, "programs", input)
	need(t, err)
	programs := result.(hostprograms.Snapshot).Programs
	if len(programs) == 0 || programs[0].Executable != executable {
		t.Fatal(programs)
	}
	if _, err := router.admin(t.Context(), f.execution.Principal, "programs", input); core.CodeOf(err) != core.Forbidden {
		t.Fatal("assistant read host inventory", err)
	}
	request := tools.ShellRequest{ComputeID: f.compute.ID, FullAccess: true, Executable: executable, Args: []string{"discovered-agent-ok"}}
	if _, err := f.b.sandbox.execute(t.Context(), f.execution, request); core.CodeOf(err) != core.Forbidden {
		t.Fatal("discovery granted access", err)
	}
	grant, err := f.b.Services.Grants.Grant(t.Context(), f.owner, policy.GrantRequest{AssistantID: f.execution.AssistantID, ResourceID: f.compute.ID, Actions: []policy.Action{policy.Execute}, Scope: policy.Scope{FullAccess: true, FullAccessScope: &policy.FullAccessScope{Mode: "programs", Programs: []policy.ProgramRule{{Executable: executable, Arguments: "exact", Args: request.Args}}}}, Mutation: core.Mutation{IdempotencyKey: "discovered-program"}})
	need(t, err)
	raw, err := router.list(t.Context(), f.execution.Principal)
	need(t, err)
	var listing resourceListing
	need(t, json.Unmarshal(raw, &listing))
	found := false
	for _, resource := range listing.Resources {
		for _, g := range resource.Grants {
			if g.ID == grant.ID {
				found = len(g.Programs) == 1 && g.Programs[0].Name == "Codex" && g.Programs[0].Executable == executable && strings.Contains(g.Programs[0].Usage, "literal args")
			}
		}
	}
	if !found {
		t.Fatal("authorized invocation guidance missing", string(raw))
	}
	raw, err = f.b.sandbox.execute(t.Context(), f.execution, request)
	need(t, err)
	if !strings.Contains(string(raw), "discovered-agent-ok") {
		t.Fatal(string(raw))
	}
	request.Args = []string{"other"}
	if _, err := f.b.sandbox.execute(t.Context(), f.execution, request); core.CodeOf(err) != core.Forbidden {
		t.Fatal("argument scope expanded", err)
	}
	need(t, f.b.Services.Grants.Revoke(t.Context(), f.owner, policy.RevokeRequest{GrantID: grant.ID, Mutation: core.Mutation{IdempotencyKey: "revoke-discovered", ExpectedVersion: grant.Version}}))
	request.Args = []string{"discovered-agent-ok"}
	if _, err := f.b.sandbox.execute(t.Context(), f.execution, request); core.CodeOf(err) != core.Forbidden {
		t.Fatal("revoked program executed", err)
	}
	raw, err = router.list(t.Context(), f.execution.Principal)
	need(t, err)
	if strings.Contains(string(raw), executable) {
		t.Fatal("revoked program path leaked")
	}
}

func TestHostProgramsDeploymentGroupAndRemoteGate(t *testing.T) {
	f := newMultiUserFixture(t, directConfig)
	input := json.RawMessage(`{"ID":"local","Command":"codex"}`)
	if _, err := f.b.remote.admin(t.Context(), f.alice, "programs", input); core.CodeOf(err) != core.Forbidden {
		t.Fatal("group bypass", err)
	}
	f.group.Resources = append(f.group.Resources, auth.ResourceReference{Kind: "sandbox", ID: "local", AllowFullAccess: true})
	_, err := f.b.Services.Auth.(*auth.Manager).SaveGroup(t.Context(), f.admin, auth.SaveGroupRequest{Group: f.group, Mutation: core.Mutation{IdempotencyKey: "allow-programs", ExpectedVersion: f.group.Version}})
	need(t, err)
	_, err = f.b.remote.admin(t.Context(), f.alice, "programs", input)
	need(t, err)
	f.b.remote.adapters.storageConfig.AllowFullAccess = false
	if _, err := f.b.remote.admin(t.Context(), f.admin, "programs", input); core.CodeOf(err) != core.Forbidden {
		t.Fatal("deployment bypass", err)
	}
	if _, err := f.b.remote.admin(t.Context(), f.admin, "programs", json.RawMessage(`{"ID":"missing-node"}`)); err == nil {
		t.Fatal("remote silently fell back to local")
	}
	cfg := testConfig(t)
	directConfig(&cfg)
	b, err := newSandboxBackend(t.Context(), cfg, quotaTestStore(t))
	need(t, err)
	t.Cleanup(func() { b.Close() })
	_, err = b.Programs(t.Context(), hostprograms.Request{Command: "codex"})
	need(t, err)
	b.cfg.Sandbox.AllowFullAccess = false
	if _, err := b.Programs(t.Context(), hostprograms.Request{}); core.CodeOf(err) != core.Forbidden {
		t.Fatal("remote deployment bypass", err)
	}
}
