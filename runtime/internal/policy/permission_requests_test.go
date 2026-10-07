package policy

import (
	"molr.ai/runtime/internal/core"
	"strings"
	"testing"
	"time"
)

func TestPermissionRequestInputBoundaries(t *testing.T) {
	now := time.Now()
	expires := now.Add(time.Hour)
	valid := func() PermissionRequestInput {
		return PermissionRequestInput{ExpiresAt: &expires, ResourceKind: "storage", ResourceID: "storage", ResourceDescription: "Reports", Actions: []Action{Write, Read, Read}, PathPrefixes: []string{"/reports", "/reports"}, Reason: "Analyze reports"}
	}
	r := valid()
	if err := r.Normalize(now); err != nil || len(r.Actions) != 2 || r.Actions[0] != Read || len(r.PathPrefixes) != 1 {
		t.Fatalf("normalization: %+v %v", r, err)
	}
	for _, change := range []func(*PermissionRequestInput){
		func(r *PermissionRequestInput) { r.Actions = []Action{Manage} },
		func(r *PermissionRequestInput) { r.PathPrefixes = []string{"/reports/../private"} },
		func(r *PermissionRequestInput) { r.ResourceKind = "compute" },
		func(r *PermissionRequestInput) { r.ModelID = "model" },
		func(r *PermissionRequestInput) {
			r.ResourceKind = "model"
			r.ResourceID = "provider"
			r.PathPrefixes = nil
			r.Actions = []Action{Read}
		},
		func(r *PermissionRequestInput) { r.Reason = " " },
		func(r *PermissionRequestInput) { r.ExpiresAt = &now },
		func(r *PermissionRequestInput) { r.ExpiresAt = nil },
		func(r *PermissionRequestInput) { r.PathPrefixes = nil },
		func(r *PermissionRequestInput) { r.PathPrefixes = []string{"/" + strings.Repeat("a", 4000)} },
	} {
		r := valid()
		change(&r)
		if err := r.Normalize(now); err == nil {
			t.Fatalf("invalid request accepted: %+v", r)
		}
	}
}

func TestGroupedRequestInheritsRootAuthorizationTime(t *testing.T) {
	now := time.Now()
	expires, other := now.Add(time.Hour), now.Add(24*time.Hour)
	for _, childExpiry := range []*time.Time{nil, &other} {
		request := PermissionRequestInput{ResourceKind: "compute", ResourceID: "compute", ResourceDescription: "Build", Actions: []Action{Execute}, Reason: "Build", ExpiresAt: &expires,
			Additional: []PermissionRequestInput{{ResourceKind: "network", ResourceID: "network", ResourceDescription: "Download", Actions: []Action{Execute}, Reason: "Download", ExpiresAt: childExpiry}}}
		if err := request.Normalize(now); err != nil {
			t.Fatal(err)
		}
		if !request.Additional[0].ExpiresAt.Equal(expires) {
			t.Fatal("group did not inherit root deadline")
		}
		request.ExpiresAt = nil
		if request.Normalize(now) == nil {
			t.Fatal("child deadline allowed a missing root deadline")
		}
	}
}

func TestPermissionRequestQuotaMetadataValidation(t *testing.T) {
	now := time.Now()
	expires := now.Add(time.Hour)
	base := PermissionRequestInput{ResourceKind: "compute", ResourceID: "compute", ResourceDescription: "Compute", Actions: []Action{Execute}, Reason: "Calculate", ExpiresAt: &expires,
		Constraints: &Constraints{Compute: &ComputeConstraints{CPUPercent: 100, MemoryBytes: 64 << 20, Processes: 8, TempBytes: 1 << 20, MaxDurationSeconds: 60}}}
	for _, tc := range []struct {
		name, field string
		change      func(*PermissionRequestInput)
	}{
		{"orphan-version", "grant_id", func(r *PermissionRequestInput) { r.GrantVersion = 1 }},
		{"temporary-outlives-authorization", "temporary", func(r *PermissionRequestInput) {
			r.Temporary = &TemporaryConstraints{Limits: *r.Constraints, Until: expires.Add(time.Hour)}
		}},
		{"wrong-constraint-kind", "constraints", func(r *PermissionRequestInput) { r.ResourceKind = "network" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.change(&r)
			err := r.Normalize(now)
			detail, ok := err.(*core.Error)
			if !ok || detail.Field != tc.field {
				t.Fatalf("expected %s error, got %v", tc.field, err)
			}
			group := base
			group.Additional = []PermissionRequestInput{r}
			err = group.Normalize(now)
			detail, ok = err.(*core.Error)
			if !ok || detail.Field != "additional_requests[0]."+tc.field {
				t.Fatalf("missing child field: %v", err)
			}
		})
	}
}
