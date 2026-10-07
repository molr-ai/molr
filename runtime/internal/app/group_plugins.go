package app

import (
	"context"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/plugins"
	"molr.ai/runtime/internal/policy"
	"slices"
	"time"
)

func groupPluginBinding(ctx context.Context, store persistence.Store, p core.Principal, id core.ID, v plugins.Installation) (plugins.Binding, bool, error) {
	g, err := groupPolicy(ctx, store, p.OwnerID)
	if err != nil {
		return plugins.Binding{}, false, err
	}
	if g.Administrator {
		for _, b := range v.Bindings {
			if b.AssistantID == id {
				return b, true, nil
			}
		}
		return plugins.Binding{}, false, nil
	}
	b := plugins.Binding{AssistantID: id, Tools: []string{}, Skills: []string{}}
	allowed := map[string]bool{}
	for _, r := range g.Resources {
		if r.Kind == "tool" {
			allowed[string(r.ID)] = true
		}
	}
	for _, t := range v.Manifest.Contributes.Tools {
		if allowed[plugins.ToolName(v, t.ID)] {
			b.Tools = append(b.Tools, t.ID)
		}
	}
	for _, sk := range v.Manifest.Contributes.Skills {
		if allowed[plugins.SkillName(v, sk.ID)] {
			b.Skills = append(b.Skills, sk.ID)
		}
	}
	needsCompute := false
	for _, t := range v.Manifest.Contributes.Tools {
		if t.Execution.Type == "shell" && slices.Contains(b.Tools, t.ID) {
			needsCompute = true
		}
	}
	if needsCompute {
		grants, err := persistence.Select[policy.Grant](ctx, store, "security.grants", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID)}})
		if err != nil {
			return b, false, err
		}
		for _, grant := range grants {
			if grant.OwnerID != p.OwnerID || grant.AssistantID != id || grant.RevokedAt != nil || (grant.ExpiresAt != nil && !grant.ExpiresAt.After(time.Now())) || !slices.Contains(grant.Actions, policy.Execute) {
				continue
			}
			r, err := resource.NewRepository(store).Get(ctx, p.OwnerID, grant.ResourceID)
			if err != nil || r.Kind != resource.Compute || r.State != resource.Available {
				continue
			}
			if _, err := groupReference(ctx, store, p.OwnerID, "sandbox", sandboxOf(r)); err == nil {
				b.ComputeID = r.ID
				break
			}
		}
	}
	return b, len(b.Tools) > 0 || len(b.Skills) > 0, nil
}
