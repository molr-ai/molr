package app

import (
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/catalog"
	"net/http/httptest"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

func TestAssistantCapabilitiesAreReadOnlyScopedAndReflectStorageGrants(t *testing.T) {
	b, err := Build(t.Context(), testConfig(t))
	need(t, err)
	defer b.Close()
	p, token := ownerSession(t, b)
	ctx := t.Context()
	id := core.ID("assistant-capabilities")
	put := func(bucket string, key core.ID, value any) {
		t.Helper()
		need(t, b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			scope := p.OwnerID
			if bucket == "business.resources" {
				scope = catalog.Scope
			}
			return tx.Put(ctx, bucket, persistence.Key(scope, key), value)
		}))
	}
	put("business.assistants", id, assistant.Assistant{ID: id, OwnerID: p.OwnerID, Name: "Capability test", State: assistant.Active, Version: 1})
	need(t, b.Services.Agent.(*agent.Engine).Lifecycle(ctx, p, id, "initialize"))
	put("business.resources", "storage", resource.Resource{ID: "storage", OwnerID: catalog.Scope, Kind: resource.Storage, State: resource.Available, Actions: []string{"read", "write"}})
	now := time.Now()
	past := now.Add(-time.Hour)
	for _, tc := range []struct {
		name        string
		grant       policy.Grant
		read, write bool
		reason      string
	}{
		{"no grant", policy.Grant{}, false, false, "permission_required"},
		{"read", policy.Grant{Actions: []policy.Action{policy.Read}}, true, false, "scoped_resources"},
		{"write", policy.Grant{Actions: []policy.Action{policy.Read, policy.Write}}, true, true, "scoped_resources"},
		{"readonly", policy.Grant{Actions: []policy.Action{policy.Read, policy.Write}, Scope: policy.Scope{ReadOnly: true}}, true, false, "scoped_resources"},
		{"expired", policy.Grant{Actions: []policy.Action{policy.Read, policy.Write}, ExpiresAt: &past}, false, false, "permission_required"},
		{"revoked", policy.Grant{Actions: []policy.Action{policy.Read, policy.Write}, RevokedAt: &past}, false, false, "permission_required"},
		{"other assistant", policy.Grant{AssistantID: "other", Actions: []policy.Action{policy.Read, policy.Write}}, false, false, "permission_required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := tc.grant
			g.ID = "grant"
			g.OwnerID = p.OwnerID
			g.ResourceID = "storage"
			if g.AssistantID == "" {
				g.AssistantID = id
			}
			put("security.grants", "grant", g)
			result, err := b.Services.Agent.Capabilities(ctx, p, id)
			need(t, err)
			entries := map[string]agent.ToolCapability{}
			for _, tool := range result.Tools {
				entries[tool.Name] = tool
			}
			if len(entries) != 21 || !entries["profile_update"].Available || !entries["shell_task"].Available || !entries["permission_list"].Available || !entries["permission_request"].Available || !entries["file_search"].Available || entries["memory_search"].Available || entries["memory_read"].Available || !entries["conversation_read"].Available || !entries["plan_list"].Available || !entries["plan_read"].Available || !entries["plan_manage"].Available {
				t.Fatalf("builtins=%+v", entries)
			}
			if entries["shell_execute"].Available || entries["shell_execute"].Reason == "available" {
				t.Fatal("unconfigured default sandbox advertised executable shell")
			}
			if !entries["file_read"].Available || !entries["file_write"].Available || (len(entries["file_read"].ResourceIDs) > 0) != tc.read || (len(entries["file_write"].ResourceIDs) > 0) != tc.write {
				t.Fatalf("storage capabilities=%+v %+v", entries["file_read"], entries["file_write"])
			}
			for _, name := range []string{"file_move", "file_delete"} {
				if !entries[name].Available || (len(entries[name].ResourceIDs) > 0) != tc.write {
					t.Fatalf("%s must reflect write grants: %+v", name, entries[name])
				}
			}
			if !result.Skills.Supported || result.Skills.Items == nil || len(result.Skills.Items) != 1 || result.Skills.Items[0].Name != "molr-docs" {
				t.Fatal("workspace Skills capability missing or unexpectedly populated")
			}
		})
	}
	put("security.grants", "grant", policy.Grant{ID: "grant", OwnerID: p.OwnerID, AssistantID: id, ResourceID: "storage", Actions: []policy.Action{policy.Read}})
	put("business.resources", "storage", resource.Resource{ID: "storage", OwnerID: catalog.Scope, Kind: resource.Storage, State: resource.Offline, Actions: []string{"read"}})
	result, err := b.Services.Agent.Capabilities(ctx, p, id)
	need(t, err)
	for _, tool := range result.Tools {
		if tool.Name == "file_read" && (!tool.Available || len(tool.ResourceIDs) > 0 || tool.Reason != "workspace_available") {
			t.Fatal("offline storage appears available")
		}
	}
	// No running assistant principal is invented by this owner-facing endpoint.
	_, err = b.Services.Agent.Capabilities(ctx, core.Principal{OwnerID: p.OwnerID, SubjectID: id, Kind: core.ActorAssistant, ExecutionID: "fake-run"}, id)
	if err == nil {
		t.Fatal("assistant inspected another execution's capabilities")
	}
	put("business.assistants", "foreign", assistant.Assistant{ID: "foreign", OwnerID: "other", State: assistant.Active})
	if _, err = b.Services.Agent.Capabilities(ctx, p, "foreign"); err == nil {
		t.Fatal("foreign capabilities exposed")
	}
	request := httptest.NewRequest("GET", "https://runtime.example/api/assistants/"+string(id)+"/capabilities", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("Cookie", "__Host-molr_session="+token.Reveal())
	response := httptest.NewRecorder()
	b.HTTP.Handler().ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("capabilities route: %d %s", response.Code, response.Body.String())
	}
	var dto agent.Capabilities
	need(t, json.Unmarshal(response.Body.Bytes(), &dto))
	if dto.AssistantID != id {
		t.Fatal("wrong assistant")
	}
	for _, bucket := range []string{"agent.operations", "agent.model_attempts", "agent.jobs"} {
		rows, err := persistence.Scan[json.RawMessage](ctx, b.store, bucket)
		need(t, err)
		if len(rows) > 0 {
			t.Fatalf("inspection created execution data in %s", bucket)
		}
	}
}
