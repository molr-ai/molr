package app

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

func (s *permissionRequests) checkExistingPermission(ctx context.Context, e core.Execution, input policy.PermissionRequestInput) error {
	now := time.Now()
	grants, err := persistence.Select[policy.Grant](ctx, s.store, "security.grants", persistence.Query{Where: []persistence.Filter{
		persistence.Eq("OwnerID", e.Principal.OwnerID), persistence.Eq("AssistantID", e.AssistantID), persistence.Eq("ResourceID", input.ResourceID), persistence.Eq("RevokedAt", nil),
	}})
	if err != nil {
		return err
	}
	for _, g := range grants {
		if g.RevokedAt != nil || g.ExpiresAt != nil && !g.ExpiresAt.After(now) {
			continue
		}
		if permissionCovered(g, input, now) {
			expiry := "no expiration"
			if g.ExpiresAt != nil {
				expiry = g.ExpiresAt.UTC().Format(time.RFC3339Nano)
			}
			return &core.Error{Code: core.InvalidArgument, Field: "resource_id", Reason: "permission_already_granted", Message: fmt.Sprintf("permission already granted: grant_id=%s, grant_version=%d, expires_at=%s covers the requested actions, scope, limits and expiration; use the existing permission, and refresh permission_list before continuing", g.ID, g.Version, expiry)}
		}
	}
	return nil
}

func permissionCovered(g policy.Grant, q policy.PermissionRequestInput, now time.Time) bool {
	if g.RevokedAt != nil || g.ResourceID != q.ResourceID || g.Scope.FullAccess != q.FullAccess || g.Scope.ModelID != q.ModelID || q.ExpiresAt == nil || !q.ExpiresAt.After(now) || g.ExpiresAt != nil && g.ExpiresAt.Before(*q.ExpiresAt) {
		return false
	}
	for _, action := range q.Actions {
		if !slices.Contains(g.Actions, action) || action == policy.Write && g.Scope.ReadOnly {
			return false
		}
	}
	for _, path := range q.PathPrefixes {
		covered := len(g.Scope.PathPrefixes) == 0
		for _, prefix := range g.Scope.PathPrefixes {
			if prefix == "/" || path == prefix || strings.HasPrefix(path, prefix+"/") {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	// Check each interval where either temporary replacement changes. A temporary
	// increase that ends before the requested deadline cannot cover the whole need.
	proposed := policy.Grant{Constraints: q.Constraints, Temporary: q.Temporary}
	boundaries := []time.Time{now}
	for _, temporary := range []*policy.TemporaryConstraints{g.Temporary, q.Temporary} {
		if temporary != nil && temporary.Until.After(now) && temporary.Until.Before(*q.ExpiresAt) {
			boundaries = append(boundaries, temporary.Until)
		}
	}
	for _, at := range boundaries {
		if !permissionLimitsCover(g.EffectiveConstraints(at), proposed.EffectiveConstraints(at)) {
			return false
		}
	}
	return true
}

func permissionLimitsCover(have, want *policy.Constraints) bool {
	// Omitted proposal limits request access without asking for a quota increase.
	if want == nil {
		return true
	}
	if have == nil {
		return want.Compute == nil || len(want.Compute.GPUDevices()) == 0
	}
	if have.Kind() != want.Kind() {
		return false
	}
	if w := want.Compute; w != nil {
		h := have.Compute
		if h.CPUPercent < w.CPUPercent || h.MemoryBytes < w.MemoryBytes || h.Processes < w.Processes || h.TempBytes < w.TempBytes || !h.UnlimitedDuration && (w.UnlimitedDuration || h.MaxDurationSeconds < w.MaxDurationSeconds) {
			return false
		}
		for _, id := range w.GPUDevices() {
			if !slices.Contains(h.GPUDevices(), id) {
				return false
			}
		}
	}
	if w := want.Network; w != nil {
		h := have.Network
		return h.Period == w.Period && h.Timezone == w.Timezone && h.Bytes >= w.Bytes && h.Connections >= w.Connections
	}
	if w := want.Storage; w != nil {
		h := have.Storage
		return h.CapacityBytes >= w.CapacityBytes && h.MaxFileBytes >= w.MaxFileBytes && h.MaxFiles >= w.MaxFiles
	}
	return true
}
