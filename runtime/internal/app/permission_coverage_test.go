package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

func TestPermissionAlreadyGrantedPublishesNothing(t *testing.T) {
	for _, grouped := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "group"}[grouped], func(t *testing.T) {
			f := newResourceAccessFixture(t, nil)
			s := newPermissionRequestsFixture(t, f)
			expiry := time.Now().Add(2 * time.Hour)
			grant := f.grant(t, f.storage.ID, "existing", []policy.Action{policy.Read, policy.Write}, policy.Scope{PathPrefixes: []string{"/read"}}, &expiry)
			input := policy.PermissionRequestInput{ResourceKind: "storage", ResourceID: f.storage.ID, ResourceDescription: "Reports", Actions: []policy.Action{policy.Read}, PathPrefixes: []string{"/read/subdir"}, ExpiresAt: futurePermissionTime(), Reason: "Read reports"}
			field := "resource_id"
			if grouped {
				duplicate := input
				input.PathPrefixes = []string{"/secret"}
				input.Additional = []policy.PermissionRequestInput{duplicate}
				field = "additional_requests[0].resource_id"
			}
			executor := tools.NewExecutor(f.b.store, tools.Options{Authorizer: f.ad.policy, Operations: operation.NewService(f.b.store, operation.Options{Authorizer: f.ad.policy}), PermissionRequest: s.create})
			raw, err := json.Marshal(input)
			need(t, err)
			result, err := executor.Execute(t.Context(), tools.Call{Execution: f.execution, Name: "permission_request", Arguments: raw})
			if err == nil || result.Failure == nil || result.Failure.Reason != "permission_already_granted" || result.Failure.Field != field || result.Failure.SideEffectsPossible || !strings.Contains(result.Failure.Message, string(grant.ID)) {
				t.Fatalf("unexpected result: %+v %v", result.Failure, err)
			}
			requests, err := s.List(t.Context(), f.owner, f.execution.AssistantID)
			need(t, err)
			messages, err := f.b.Services.Conversations.Messages(t.Context(), f.owner, conversation.MessageQuery{ConversationID: "permission-chat"})
			need(t, err)
			notices, err := notification.New(f.b.store, f.ad.policy).List(t.Context(), f.owner, notification.Query{Kind: "permission.request", Filter: "active"})
			need(t, err)
			receipts, err := persistence.Scan[core.ID](t.Context(), f.b.store, "security.permission_operations")
			need(t, err)
			if len(requests)+len(messages.Items)+len(notices.Items)+len(receipts) != 0 {
				t.Fatal("duplicate request published side effects")
			}
		})
	}
}

func TestPermissionCoverageScopeExpiryAndLimits(t *testing.T) {
	now := time.Now()
	end := now.Add(time.Hour)
	later := end.Add(time.Hour)
	early := now.Add(time.Minute)
	for _, tc := range []struct {
		name    string
		change  func(*policy.Grant, *policy.PermissionRequestInput)
		covered bool
	}{
		{"same", func(g *policy.Grant, q *policy.PermissionRequestInput) {}, true},
		{"permanent", func(g *policy.Grant, q *policy.PermissionRequestInput) { g.ExpiresAt = nil }, true},
		{"longer-request", func(g *policy.Grant, q *policy.PermissionRequestInput) { q.ExpiresAt = &later }, false},
		{"revoked", func(g *policy.Grant, q *policy.PermissionRequestInput) { g.RevokedAt = &now }, false},
		{"expired", func(g *policy.Grant, q *policy.PermissionRequestInput) { g.ExpiresAt = &now }, false},
		{"other-resource", func(g *policy.Grant, q *policy.PermissionRequestInput) { q.ResourceID = "other" }, false},
		{"more-actions", func(g *policy.Grant, q *policy.PermissionRequestInput) { q.Actions = append(q.Actions, policy.Write) }, false},
		{"path-boundary", func(g *policy.Grant, q *policy.PermissionRequestInput) { q.PathPrefixes = []string{"/read-secret"} }, false},
		{"child-path", func(g *policy.Grant, q *policy.PermissionRequestInput) { q.PathPrefixes = []string{"/read/sub"} }, true},
		{"full-access-separate", func(g *policy.Grant, q *policy.PermissionRequestInput) { q.FullAccess = true }, false},
		{"exact-model", func(g *policy.Grant, q *policy.PermissionRequestInput) { q.ModelID = "other-model" }, false},
		{"larger-quota", func(g *policy.Grant, q *policy.PermissionRequestInput) {
			q.Constraints = &policy.Constraints{Compute: &policy.ComputeConstraints{MaxDurationSeconds: 120}}
		}, false},
		{"smaller-quota", func(g *policy.Grant, q *policy.PermissionRequestInput) {
			q.Constraints = &policy.Constraints{Compute: &policy.ComputeConstraints{MaxDurationSeconds: 30}}
		}, true},
		{"temporary-too-short", func(g *policy.Grant, q *policy.PermissionRequestInput) {
			g.Temporary = &policy.TemporaryConstraints{Until: early, Limits: policy.Constraints{Compute: &policy.ComputeConstraints{MaxDurationSeconds: 120}}}
			q.Constraints = &policy.Constraints{Compute: &policy.ComputeConstraints{MaxDurationSeconds: 120}}
		}, false},
		{"temporary-covers", func(g *policy.Grant, q *policy.PermissionRequestInput) {
			g.Temporary = &policy.TemporaryConstraints{Until: end, Limits: policy.Constraints{Compute: &policy.ComputeConstraints{MaxDurationSeconds: 120}}}
			q.Constraints = &policy.Constraints{Compute: &policy.ComputeConstraints{MaxDurationSeconds: 120}}
		}, true},
		{"temporary-request", func(g *policy.Grant, q *policy.PermissionRequestInput) {
			q.Temporary = &policy.TemporaryConstraints{Until: early, Limits: policy.Constraints{Compute: &policy.ComputeConstraints{MaxDurationSeconds: 120}}}
		}, false},
		{"temporary-lower-then-base", func(g *policy.Grant, q *policy.PermissionRequestInput) {
			q.Constraints = &policy.Constraints{Compute: &policy.ComputeConstraints{MaxDurationSeconds: 120}}
			q.Temporary = &policy.TemporaryConstraints{Until: early, Limits: policy.Constraints{Compute: &policy.ComputeConstraints{MaxDurationSeconds: 30}}}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := policy.Grant{ResourceID: "resource", Actions: []policy.Action{policy.Read}, Scope: policy.Scope{PathPrefixes: []string{"/read"}}, ExpiresAt: &end, Constraints: &policy.Constraints{Compute: &policy.ComputeConstraints{MaxDurationSeconds: 60}}}
			q := policy.PermissionRequestInput{ResourceID: g.ResourceID, Actions: []policy.Action{policy.Read}, PathPrefixes: []string{"/read"}, ExpiresAt: &end, Constraints: &policy.Constraints{Compute: &policy.ComputeConstraints{MaxDurationSeconds: 60}}}
			tc.change(&g, &q)
			if got := permissionCovered(g, q, now); got != tc.covered {
				t.Fatalf("covered=%v want %v", got, tc.covered)
			}
		})
	}
}

func TestPermissionCoverageQuotaDimensions(t *testing.T) {
	for _, tc := range []struct {
		name       string
		have, want *policy.Constraints
		covered    bool
	}{
		{"network smaller", &policy.Constraints{Network: &policy.NetworkConstraints{Bytes: 1000, Connections: 10, Period: "day", Timezone: "UTC"}}, &policy.Constraints{Network: &policy.NetworkConstraints{Bytes: 500, Connections: 5, Period: "day", Timezone: "UTC"}}, true},
		{"network period", &policy.Constraints{Network: &policy.NetworkConstraints{Bytes: 1000, Connections: 10, Period: "day", Timezone: "UTC"}}, &policy.Constraints{Network: &policy.NetworkConstraints{Bytes: 500, Connections: 5, Period: "month", Timezone: "UTC"}}, false},
		{"network timezone", &policy.Constraints{Network: &policy.NetworkConstraints{Bytes: 1000, Connections: 10, Period: "day", Timezone: "UTC"}}, &policy.Constraints{Network: &policy.NetworkConstraints{Bytes: 500, Connections: 5, Period: "day", Timezone: "Asia/Shanghai"}}, false},
		{"network larger", &policy.Constraints{Network: &policy.NetworkConstraints{Bytes: 1000, Connections: 10, Period: "day", Timezone: "UTC"}}, &policy.Constraints{Network: &policy.NetworkConstraints{Bytes: 2000, Connections: 5, Period: "day", Timezone: "UTC"}}, false},
		{"native network", nil, nil, true},
		{"workspace covered", &policy.Constraints{Storage: &policy.StorageConstraints{CapacityBytes: 100, MaxFileBytes: 50, MaxFiles: 10}}, &policy.Constraints{Storage: &policy.StorageConstraints{CapacityBytes: 90, MaxFileBytes: 40, MaxFiles: 9}}, true},
		{"workspace more files", &policy.Constraints{Storage: &policy.StorageConstraints{CapacityBytes: 100, MaxFileBytes: 50, MaxFiles: 10}}, &policy.Constraints{Storage: &policy.StorageConstraints{CapacityBytes: 90, MaxFileBytes: 40, MaxFiles: 11}}, false},
		{"compute unlimited", &policy.Constraints{Compute: &policy.ComputeConstraints{UnlimitedDuration: true}}, &policy.Constraints{Compute: &policy.ComputeConstraints{MaxDurationSeconds: 60}}, true},
		{"compute wants unlimited", &policy.Constraints{Compute: &policy.ComputeConstraints{MaxDurationSeconds: 60}}, &policy.Constraints{Compute: &policy.ComputeConstraints{UnlimitedDuration: true}}, false},
		{"compute more memory", &policy.Constraints{Compute: &policy.ComputeConstraints{MaxDurationSeconds: 60, MemoryBytes: 100}}, &policy.Constraints{Compute: &policy.ComputeConstraints{MaxDurationSeconds: 60, MemoryBytes: 200}}, false},
		{"compute GPU scope", &policy.Constraints{Compute: &policy.ComputeConstraints{UnlimitedDuration: true, GPU: &core.GPUAccess{DeviceIDs: []string{"gpu-a"}}}}, &policy.Constraints{Compute: &policy.ComputeConstraints{MaxDurationSeconds: 60, GPU: &core.GPUAccess{DeviceIDs: []string{"gpu-b"}}}}, false},
		{"compute GPU subset", &policy.Constraints{Compute: &policy.ComputeConstraints{UnlimitedDuration: true, GPU: &core.GPUAccess{DeviceIDs: []string{"gpu-a", "gpu-b"}}}}, &policy.Constraints{Compute: &policy.ComputeConstraints{MaxDurationSeconds: 60, GPU: &core.GPUAccess{DeviceIDs: []string{"gpu-a"}}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := permissionLimitsCover(tc.have, tc.want); got != tc.covered {
				t.Fatalf("covered=%v want %v", got, tc.covered)
			}
		})
	}
}
