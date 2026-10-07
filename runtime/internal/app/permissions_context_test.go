package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

func TestPermissionContextTracksGrantChangesAndExpiration(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	now := time.Now()
	expires := now.Add(time.Hour)
	before, err := permissionsContext(t.Context(), f.b.store, f.execution.Principal, now)
	need(t, err)
	g := f.grant(t, f.storage.ID, "context-grant", []policy.Action{policy.Read}, policy.Scope{PathPrefixes: []string{"/read"}}, &expires)
	added, err := permissionsContext(t.Context(), f.b.store, f.execution.Principal, now)
	need(t, err)
	if before.Revision == added.Revision || !strings.Contains(added.Content, string(g.ID)) {
		t.Fatal("new grant not injected")
	}
	unchanged, err := permissionsContext(t.Context(), f.b.store, f.execution.Principal, now.Add(time.Minute))
	need(t, err)
	if unchanged.Revision != added.Revision {
		t.Fatal("clock alone changed context")
	}
	updated, err := f.b.Services.Grants.UpdateGrant(t.Context(), f.owner, policy.UpdateGrantRequest{Mutation: core.Mutation{IdempotencyKey: "context-update", ExpectedVersion: g.Version}, GrantID: g.ID, Actions: []policy.Action{policy.Read, policy.Write}, Scope: policy.Scope{PathPrefixes: []string{"/write"}}, ExpiresAt: &expires})
	need(t, err)
	changed, err := permissionsContext(t.Context(), f.b.store, f.execution.Principal, now)
	need(t, err)
	if changed.Revision == added.Revision || !strings.Contains(changed.Content, "/write") || strings.Contains(changed.Content, "/read") {
		t.Fatal("grant modification did not replace scope")
	}
	expired, err := permissionsContext(t.Context(), f.b.store, f.execution.Principal, expires)
	need(t, err)
	if expired.Revision == changed.Revision || strings.Contains(expired.Content, string(g.ID)) {
		t.Fatal("expiry left stale authorization")
	}
	need(t, f.b.Services.Grants.Revoke(t.Context(), f.owner, policy.RevokeRequest{Mutation: core.Mutation{IdempotencyKey: "context-revoke", ExpectedVersion: updated.Version}, GrantID: g.ID}))
	revoked, err := permissionsContext(t.Context(), f.b.store, f.execution.Principal, now)
	need(t, err)
	if revoked.Revision != before.Revision {
		t.Fatal("revocation retained grant")
	}
}

func TestRequestablePermissionsExposeChoicesWithoutGrantingAccess(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	need(t, f.b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.resources", "other/hidden", resource.Resource{ID: "hidden", OwnerID: "other", Name: "private-resource", Kind: resource.Storage, Actions: []string{"read"}, State: resource.Available})
	}))
	out := f.list(t)
	found := false
	for _, row := range out.Requestable {
		if row.ResourceID == f.storage.ID {
			found = true
			if len(row.Actions) == 0 || len(row.PathPrefixes) != 1 || row.PathPrefixes[0] != "/" {
				t.Fatal("missing requestable scope")
			}
		}
	}
	if !found {
		t.Fatal("ungranted storage cannot be discovered")
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "private-resource") || strings.Contains(string(raw), f.root) {
		t.Fatal("discovery leaked private data")
	}
	err := f.ad.policy.Require(t.Context(), f.execution.Principal, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: f.storage.ID}, ResourceID: f.storage.ID, Action: policy.Read, Destination: "/read"})
	if err == nil {
		t.Fatal("requestable resource became authorized")
	}
}

func TestPermissionOwnerCanEditProposedScopeAndTime(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	s := newPermissionRequestsFixture(t, f)
	raw, err := s.create(t.Context(), f.execution, policy.PermissionRequestInput{ResourceKind: "storage", ResourceID: f.storage.ID, ResourceDescription: "Reports", Actions: []policy.Action{policy.Read}, PathPrefixes: []string{"/read"}, ExpiresAt: futurePermissionTime(), Reason: "Read reports"})
	need(t, err)
	var request policy.PermissionRequest
	need(t, json.Unmarshal(raw, &request))
	until := time.Now().Add(2 * time.Hour)
	approved, err := s.Resolve(t.Context(), f.owner, policy.PermissionResolution{Mutation: core.Mutation{IdempotencyKey: "edit-approve", ExpectedVersion: 1}, RequestID: request.ID, Decision: "approve", ResourceID: f.storage.ID, Actions: []policy.Action{policy.Read, policy.Write}, Scope: policy.Scope{PathPrefixes: []string{"/write"}}, ExpiresAt: &until})
	need(t, err)
	if approved.Granted == nil || len(approved.Granted.Actions) != 2 || approved.Granted.Scope.PathPrefixes[0] != "/write" || !approved.Granted.ExpiresAt.Equal(until) {
		t.Fatal("owner edits were not applied")
	}
	if request.Input.ExpiresAt.Equal(until) || approved.Input.PathPrefixes[0] != "/read" {
		t.Fatal("proposal history overwritten")
	}
}
