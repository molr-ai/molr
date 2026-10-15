package app

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/agent/prompt"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	"strings"
	"testing"
	"time"
)

func TestAutomaticNetworkSelectionFollowsLiveAssistantAndSandboxGrants(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	r, err := f.b.Services.Resources.Connect(t.Context(), f.owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "network-auto"}, Kind: resource.Network, Provider: "sandbox-network", Name: "Internet", Binding: resource.Binding{SandboxTarget: testNetworkPreset()}})
	need(t, err)
	ids, err := automaticNetworks(t.Context(), f.b.store, f.execution, "local")
	need(t, err)
	if len(ids) != 0 {
		t.Fatal("requestable resource became authority")
	}
	g := f.grant(t, r.ID, "grant-auto", []policy.Action{policy.Execute}, policy.Scope{}, nil)
	ids, err = automaticNetworks(t.Context(), f.b.store, f.execution, "local")
	need(t, err)
	if len(ids) != 1 || ids[0] != r.ID {
		t.Fatal(ids)
	}
	ids, err = automaticNetworks(t.Context(), f.b.store, f.execution, "remote")
	need(t, err)
	if len(ids) != 0 {
		t.Fatal("cross-sandbox grant selected")
	}
	need(t, f.b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		past := time.Now().Add(-time.Second)
		g.ExpiresAt = &past
		return tx.Put(ctx, "security.grants", persistence.Key(g.OwnerID, g.ID), g)
	}))
	ids, err = automaticNetworks(t.Context(), f.b.store, f.execution, "local")
	need(t, err)
	if len(ids) != 0 {
		t.Fatal("expired grant selected")
	}
}
func TestPermissionQuotaProjectionPreservesAuthoritativeLimitsAndVersion(t *testing.T) {
	raw := map[string]any{"Resources": []any{map[string]any{"Grants": []any{permissionGrant{Version: 7, Constraints: &policy.Constraints{Network: &policy.NetworkConstraints{Bytes: 1 << 30, Connections: 32, Period: "day", Timezone: "Asia/Shanghai"}}}}}}, "Requestable": []any{}, "BuiltInStorage": []any{}}
	data, _ := json.Marshal(raw)
	text, err := prompt.Structured("permission_list", string(data))
	need(t, err)
	for _, want := range []string{`"Version":7`, `"bytes":1073741824`, `"period":"day"`, `"connections":32`} {
		if !strings.Contains(text, want) {
			t.Fatal("quota stripped", want, text)
		}
	}
}
func TestComputeQuotaOverridesDeploymentDefaultsAndBoundsDuration(t *testing.T) {
	c := platform.Command{Timeout: 0, Limits: platform.Limits{MemoryBytes: 512 << 20}}
	q := policy.ComputeConstraints{CPUPercent: 200, MemoryBytes: 2 << 30, Processes: 128, TempBytes: 64 << 20, MaxDurationSeconds: 1800}
	max := platform.Limits{CPUPercent: 400, MemoryBytes: 4 << 30, Processes: 256, TempBytes: 1 << 30}
	need(t, applyComputeConstraints(&c, &q, max))
	if !c.ComputeQuota || c.Limits.MemoryBytes != 2<<30 || c.Timeout != 30*time.Minute {
		t.Fatal(c)
	}
	q.MemoryBytes = 8 << 30
	if applyComputeConstraints(&c, &q, max) == nil {
		t.Fatal("deployment ceiling bypassed")
	}
}

func TestWorkspaceRegrantUsesCurrentAuthorizationInsteadOfHistoricalVersion(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	id := workspaceResourceID(f.owner.OwnerID, "local")
	now := time.Now()
	old := policy.Grant{ID: "old-storage", OwnerID: f.owner.OwnerID, AssistantID: f.execution.AssistantID, ResourceID: id, Actions: []policy.Action{policy.Write}, Constraints: &policy.Constraints{Storage: &policy.StorageConstraints{CapacityBytes: 1 << 30, MaxFileBytes: 1 << 20, MaxFiles: 100}}, Version: 20, RevokedAt: &now, UpdatedAt: now}
	current := old
	current.ID = "new-storage"
	current.Version = 1
	current.RevokedAt = nil
	current.UpdatedAt = now.Add(time.Second)
	current.Constraints = &policy.Constraints{Storage: &policy.StorageConstraints{CapacityBytes: 2 << 30, MaxFileBytes: 1 << 20, MaxFiles: 200}}
	need(t, f.b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, g := range []policy.Grant{old, current} {
			if err := tx.Put(ctx, "security.grants", persistence.Key(g.OwnerID, g.ID), g); err != nil {
				return err
			}
		}
		return nil
	}))
	limits, err := workspaceConstraints(t.Context(), f.b.store, f.owner.OwnerID, f.execution.AssistantID, "local")
	need(t, err)
	if limits.CapacityBytes != 2<<30 {
		t.Fatal("old grant version replaced new authorization", limits)
	}
}

func TestNetworkQuotaRejectsMissingLimitsAndDeduplicatesAliases(t *testing.T) {
	if platform.NativeSandbox() {
		t.Skip("requires Linux hard quota grants; native grants are covered by TestNativeSandboxGrantRoutingAndRevocation")
	}
	f := newResourceAccessFixture(t, nil)
	connect := func(key string) resource.Resource {
		r, err := f.b.Services.Resources.Connect(t.Context(), f.owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: key}, Kind: resource.Network, Provider: "sandbox-network", Name: key, Binding: resource.Binding{SandboxTarget: testNetworkPreset()}})
		need(t, err)
		return r
	}
	a, b := connect("quota-source-a"), connect("quota-source-b")
	constraints := &policy.Constraints{Network: &policy.NetworkConstraints{Bytes: 1 << 30, Connections: 32, Period: "day", Timezone: "UTC"}}
	_, err := f.b.Services.Grants.Grant(t.Context(), f.owner, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "quota-first"}, AssistantID: f.execution.AssistantID, ResourceID: a.ID, Actions: []policy.Action{policy.Execute}, Constraints: constraints})
	need(t, err)
	request := policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "legacy-alias"}, AssistantID: f.execution.AssistantID, ResourceID: b.ID, Actions: []policy.Action{policy.Execute}}
	if _, err = f.b.Services.Grants.Grant(t.Context(), f.owner, request); err == nil {
		t.Fatal("legacy alias bypassed a migrated quota source")
	}
	request.Mutation.IdempotencyKey = "quota-second"
	request.Constraints = constraints
	_, err = f.b.Services.Grants.Grant(t.Context(), f.owner, request)
	need(t, err)
	ids, err := automaticNetworks(t.Context(), f.b.store, f.execution, "local")
	need(t, err)
	if len(ids) != 1 || ids[0] != b.ID {
		t.Fatal("source not deduplicated to latest approved quota", ids)
	}
}

func TestQuotaGrantsRequireExplicitLimits(t *testing.T) {
	if platform.NativeSandbox() {
		t.Skip("requires Linux hard quota grants; native grants are covered by TestNativeSandboxGrantRoutingAndRevocation")
	}
	f := newResourceAccessFixture(t, nil)
	network, err := f.b.Services.Resources.Connect(t.Context(), f.owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "network"}, Kind: resource.Network, Provider: "sandbox-network", Name: "Internet", Binding: resource.Binding{SandboxTarget: testNetworkPreset()}})
	need(t, err)
	need(t, ensureWorkspaceResource(t.Context(), f.b.store, f.owner.OwnerID, "local"))
	for _, id := range []core.ID{f.compute.ID, network.ID, workspaceResourceID(f.owner.OwnerID, "local")} {
		actions := []policy.Action{policy.Execute}
		if id == workspaceResourceID(f.owner.OwnerID, "local") {
			actions = []policy.Action{policy.Write}
		}
		_, err := f.b.Services.Grants.Grant(t.Context(), f.owner, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: string(id)}, AssistantID: f.execution.AssistantID, ResourceID: id, Actions: actions})
		if core.CodeOf(err) != core.InvalidArgument {
			t.Fatalf("missing limits accepted for %s: %v", id, err)
		}
	}
	grants, err := f.b.Services.Grants.List(t.Context(), f.owner, policy.Query{ResourceID: f.compute.ID})
	need(t, err)
	g := grants.Items[0]
	_, err = f.b.Services.Grants.UpdateGrant(t.Context(), f.owner, policy.UpdateGrantRequest{Mutation: core.Mutation{IdempotencyKey: "remove-limits", ExpectedVersion: g.Version}, GrantID: g.ID, Actions: g.Actions})
	if core.CodeOf(err) != core.InvalidArgument {
		t.Fatalf("removed limits: %v", err)
	}
	// Execution requires explicit limits even if a malformed record is present.
	need(t, f.b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		g.Constraints = nil
		return tx.Put(ctx, "security.grants", persistence.Key(g.OwnerID, g.ID), g)
	}))
	if _, err := executionGrant(t.Context(), f.b.store, f.execution, f.compute.ID); core.CodeOf(err) != core.Forbidden {
		t.Fatalf("execution without limits authorized: %v", err)
	}
}
