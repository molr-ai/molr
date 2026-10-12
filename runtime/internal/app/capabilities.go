package app

import (
	"context"
	"slices"
	"strings"
	"time"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

func inspectCapabilities(ctx context.Context, store persistence.Store, catalog tools.Catalog, p core.Principal, id core.ID, now time.Time) (agent.Capabilities, error) {
	out := agent.Capabilities{AssistantID: id, Tools: []agent.ToolCapability{}}
	out.Skills.Items = []agent.SkillInfo{} // Workspace skills are supplied by the composition after authorization.
	err := store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		a, err := persistence.Load[assistant.Assistant](ctx, store, "business.assistants", persistence.Key(p.OwnerID, id))
		if err != nil {
			return err
		}
		if a.OwnerID != p.OwnerID {
			return denied()
		}
		definitions, err := catalog.Catalog(ctx, p)
		if err != nil {
			return err
		}
		grants, err := persistence.Select[policy.Grant](ctx, store, "security.grants", persistence.Query{Where: []persistence.Filter{persistence.Eq("RevokedAt", nil), persistence.Eq("OwnerID", p.OwnerID), persistence.Eq("AssistantID", id)}, Any: [][]persistence.Filter{{persistence.Eq("ExpiresAtKey", "")}, {{Field: "ExpiresAtKey", Op: ">", Value: persistence.TimeKey(now)}}}})
		if err != nil {
			return err
		}
		state, err := persistence.Load[agent.Status](ctx, store, "agent.status", persistence.Key(p.OwnerID, id))
		if err != nil && !persistence.Missing(err) {
			return err
		}
		for _, definition := range definitions {
			item := agent.ToolCapability{Definition: definition, Available: true, Reason: "available", ResourceIDs: []core.ID{}}
			switch definition.Name {
			case "file_read", "file_list", "file_write", "file_edit", "file_move", "file_delete", "shell_execute", "browser_session":
				action := policy.Read
				kind := resource.Storage
				if definition.Name == "shell_execute" || definition.Name == "browser_session" {
					action = policy.Execute
					kind = resource.Compute
				}
				if definition.Name == "file_write" || definition.Name == "file_edit" || definition.Name == "file_move" || definition.Name == "file_delete" {
					action = policy.Write
				}
				item.Available = false
				item.Reason = "permission_required"
				for _, g := range grants {
					if g.OwnerID != p.OwnerID || g.AssistantID != id || g.RevokedAt != nil || (g.ExpiresAt != nil && !g.ExpiresAt.After(now)) || !slices.Contains(g.Actions, action) || (action == policy.Write && g.Scope.ReadOnly) {
						continue
					}
					r, err := resource.NewRepository(store).Get(ctx, p.OwnerID, g.ResourceID)
					if persistence.Missing(err) {
						continue
					}
					if err != nil {
						return err
					}
					// These fixed tools operate on storage paths; a model grant does not
					// authorize file access. Path restrictions remain enforced at execution.
					if !catalogOwned(ctx, store, r.OwnerID, p.OwnerID) || r.Kind != kind || g.Scope.ModelID != "" || ((definition.Name == "shell_execute" || definition.Name == "browser_session") && r.Provider != "local-sandbox" && r.Provider != "remote-compute") {
						continue
					}
					if r.State != resource.Available || !slices.Contains(r.Actions, string(action)) {
						if !item.Available {
							item.Reason = "resource_unavailable"
						}
						continue
					}
					if definition.Name == "file_edit" {
						readable := false
						for _, read := range grants {
							if read.OwnerID == p.OwnerID && read.AssistantID == id && read.ResourceID == r.ID && read.RevokedAt == nil && (read.ExpiresAt == nil || read.ExpiresAt.After(now)) && slices.Contains(read.Actions, policy.Read) && read.Scope.ModelID == "" && scopesOverlap(read.Scope.PathPrefixes, g.Scope.PathPrefixes) {
								readable = true
								break
							}
						}
						if !readable || !slices.Contains(r.Actions, "read") {
							continue
						}
					}
					item.Available = true
					item.Reason = "scoped_resources"
					if !slices.Contains(item.ResourceIDs, r.ID) {
						item.ResourceIDs = append(item.ResourceIDs, r.ID)
					}
				}
				if definition.Name != "shell_execute" && definition.Name != "browser_session" {
					item.Available = true
					item.Reason = "workspace_available"
					if len(item.ResourceIDs) > 0 {
						item.Reason = "workspace_and_resources"
					}
				}
			}
			if a.State != assistant.Active {
				item.Available = false
				item.Reason = "assistant_archived"
			} else if state.State == agent.Stopped || state.State == agent.Paused {
				item.Available = false
				item.Reason = "assistant_stopped"
			}
			out.Tools = append(out.Tools, item)
		}
		return nil
	})
	return out, err
}

func scopesOverlap(a, b []string) bool {
	if len(a) == 0 {
		a = []string{"/"}
	}
	if len(b) == 0 {
		b = []string{"/"}
	}
	for _, left := range a {
		for _, right := range b {
			if left == right || strings.HasPrefix(left, strings.TrimSuffix(right, "/")+"/") || strings.HasPrefix(right, strings.TrimSuffix(left, "/")+"/") {
				return true
			}
		}
	}
	return false
}
