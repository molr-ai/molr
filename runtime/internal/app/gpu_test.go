package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/prompt"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	sm "molr.ai/runtime/internal/sandboxmanager"
)

const gpuA = "GPU-00000000-0000-0000-0000-000000000001"
const gpuB = "GPU-00000000-0000-0000-0000-000000000002"

func TestGPUComputeGrantAutomaticallyBuildsPlanAndRestoresTemporaryScope(t *testing.T) {
	if platform.NativeSandbox() {
		t.Skip("requires Linux GPU device grants; macOS rejects GPU configuration")
	}
	f := newResourceAccessFixture(t, func(c *config.Config, _ string) {
		c.Sandbox.GPU = platform.GPUConfig{Enabled: true, Devices: []string{gpuA}}
	})
	grants, err := f.b.Services.Grants.List(t.Context(), f.owner, policy.Query{ResourceID: f.compute.ID})
	need(t, err)
	g := grants.Items[0]
	temporary := *g.Constraints.Compute
	temporary.GPU = &core.GPUAccess{DeviceIDs: []string{gpuA}}
	until := time.Now().Add(time.Hour)
	g, err = f.b.Services.Grants.UpdateGrant(t.Context(), f.owner, policy.UpdateGrantRequest{Mutation: core.Mutation{IdempotencyKey: "gpu-temporary", ExpectedVersion: g.Version}, GrantID: g.ID, Actions: g.Actions, Constraints: g.Constraints, Temporary: &policy.TemporaryConstraints{Limits: policy.Constraints{Compute: &temporary}, Until: until}})
	need(t, err)
	req := tools.ShellRequest{ComputeID: f.compute.ID, Script: "echo ready"}
	c, err := f.b.sandbox.plan(t.Context(), f.execution, req)
	need(t, err)
	if len(c.GPUDeviceIDs) != 1 || c.GPUDeviceIDs[0] != gpuA {
		t.Fatalf("GPU not inherited: %+v", c.GPUDeviceIDs)
	}
	need(t, f.b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		g.Temporary.Until = time.Now().Add(-time.Second)
		return tx.Put(ctx, "security.grants", persistence.Key(g.OwnerID, g.ID), g)
	}))
	c, err = f.b.sandbox.plan(t.Context(), f.execution, req)
	need(t, err)
	if len(c.GPUDeviceIDs) != 0 {
		t.Fatal("expired temporary GPU survives")
	}
	temporary.GPU.DeviceIDs = []string{gpuB}
	_, err = f.b.Services.Grants.UpdateGrant(t.Context(), f.owner, policy.UpdateGrantRequest{Mutation: core.Mutation{IdempotencyKey: "gpu-outside", ExpectedVersion: g.Version}, GrantID: g.ID, Actions: g.Actions, Constraints: &policy.Constraints{Compute: &temporary}})
	if core.ReasonOf(err) != "gpu_scope_denied" {
		t.Fatalf("deployment GPU scope bypassed: %v", err)
	}
}
func TestGPUGroupCatalogDoesNotExpandOrLeakDevices(t *testing.T) {
	info := sm.Info{AuthorizationCeilings: platform.Limits{GPUDeviceIDs: []string{gpuA, gpuB}}, GPUs: []platform.GPUDevice{{ID: gpuA}, {ID: gpuB}}}
	info.HostCapacity.GPUs = []platform.GPUDevice{{ID: gpuB, Name: "Telemetry only", Reason: "gpu_platform_unsupported"}}
	for _, ref := range []*auth.ResourceReference{{}, {Quota: auth.ResourceQuota{Compute: &core.ComputeConstraints{}}}} {
		out := groupGPUInfo(info, ref)
		if len(out.GPUs) != 0 || len(out.AuthorizationCeilings.GPUDeviceIDs) != 0 {
			t.Fatal("default group gained GPU")
		}
		if len(out.HostCapacity.GPUs) != 1 || out.HostCapacity.GPUs[0].Available {
			t.Fatal("hardware telemetry was lost or became executable")
		}
	}
	ref := &auth.ResourceReference{Quota: auth.ResourceQuota{Compute: &core.ComputeConstraints{GPU: &core.GPUAccess{DeviceIDs: []string{gpuA}}}}}
	out := groupGPUInfo(info, ref)
	if len(out.GPUs) != 1 || out.GPUs[0].ID != gpuA || len(info.GPUs) != 2 {
		t.Fatal("group filtering mutated source or leaked GPU")
	}
}
func TestGPUPermissionContextPreservesHardwareScope(t *testing.T) {
	raw := map[string]any{"Resources": []any{map[string]any{"Grants": []any{permissionGrant{Constraints: &policy.Constraints{Compute: &core.ComputeConstraints{GPU: &core.GPUAccess{DeviceIDs: []string{gpuA}}}}}}}}, "Requestable": []any{map[string]any{"ResourceID": "compute", "GPUs": []platform.GPUDevice{{ID: gpuA, Name: "Example GPU", Available: true}}, "AuthorizationCeilings": platform.Limits{GPUDeviceIDs: []string{gpuA}}}}, "BuiltInStorage": []any{}}
	data, _ := json.Marshal(raw)
	text, err := prompt.Structured("permission_list", string(data))
	need(t, err)
	for _, want := range []string{`"gpu"`, `"device_ids"`, `"GPUDeviceIDs"`, `"GPUs"`, `"Example GPU"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("context stripped %s: %s", want, text)
		}
	}
}

func TestGPUAssistantRequestRequiresApprovalAndOwnerCanNarrow(t *testing.T) {
	if platform.NativeSandbox() {
		t.Skip("requires Linux GPU device grants; macOS rejects GPU configuration")
	}
	f := newResourceAccessFixture(t, func(c *config.Config, _ string) {
		c.Sandbox.GPU = platform.GPUConfig{Enabled: true, Devices: []string{gpuA, gpuB}}
	})
	requests := newPermissionRequestsFixture(t, f)
	grants, err := f.b.Services.Grants.List(t.Context(), f.owner, policy.Query{ResourceID: f.compute.ID})
	need(t, err)
	g := grants.Items[0]
	proposed := *g.Constraints.Compute
	proposed.GPU = &core.GPUAccess{DeviceIDs: []string{gpuA, gpuB}}
	expires := time.Now().Add(time.Hour)
	input := policy.PermissionRequestInput{ResourceKind: "compute", ResourceID: f.compute.ID, ResourceDescription: "GPU compute", Actions: []policy.Action{policy.Execute}, GrantID: g.ID, GrantVersion: g.Version, Constraints: &policy.Constraints{Compute: &proposed}, ExpiresAt: &expires, Reason: "Run the GPU task"}
	raw, err := requests.create(t.Context(), f.execution, input)
	need(t, err)
	var request policy.PermissionRequest
	need(t, json.Unmarshal(raw, &request))
	current, err := executionGrant(t.Context(), f.b.store, f.execution, f.compute.ID)
	need(t, err)
	if len(current.Constraints.Compute.GPUDevices()) != 0 {
		t.Fatal("request granted GPU before approval")
	}
	approved := proposed
	approved.GPU = &core.GPUAccess{DeviceIDs: []string{gpuA}}
	decision, err := requests.Resolve(t.Context(), f.owner, policy.PermissionResolution{Mutation: core.Mutation{IdempotencyKey: "gpu-approval", ExpectedVersion: request.Version}, RequestID: request.ID, Decision: "approve", ResourceID: f.compute.ID, Actions: []policy.Action{policy.Execute}, GrantID: g.ID, GrantVersion: g.Version, Constraints: &policy.Constraints{Compute: &approved}, ExpiresAt: &expires})
	need(t, err)
	if decision.Granted == nil || len(decision.Granted.Constraints.Compute.GPUDevices()) != 1 {
		t.Fatal("owner GPU selection lost")
	}
	c, err := f.b.sandbox.plan(t.Context(), f.execution, tools.ShellRequest{ComputeID: f.compute.ID, Script: "true"})
	need(t, err)
	if len(c.GPUDeviceIDs) != 1 || c.GPUDeviceIDs[0] != gpuA {
		t.Fatal("requested scope used instead of final approval")
	}
}

func TestSharedGPUComputeContextDoesNotAdvertiseDeviceGrants(t *testing.T) {
	descriptor := requestablePermission{ResourceID: "compute", GPUAccessMode: "shared", SharedGPUs: []platform.GPUDevice{{ID: gpuA, Name: "Shared GPU", MemoryBytes: 16 << 30}}}
	data, err := json.Marshal(map[string]any{"Resources": []any{}, "BuiltInStorage": []any{}, "Requestable": []requestablePermission{descriptor}})
	need(t, err)
	text, err := prompt.Structured("permission_list", string(data))
	need(t, err)
	for _, want := range []string{`"GPUAccessMode":"shared"`, `"SharedGPUs"`, `"Shared GPU"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("shared GPU semantics stripped: %s", text)
		}
	}
	if strings.Contains(text, `"device_ids"`) || strings.Contains(text, `"GPUDeviceIDs"`) {
		t.Fatalf("shared inventory became a device grant: %s", text)
	}
}
