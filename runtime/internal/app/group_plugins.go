package app

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/plugins"
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
	b := plugins.Binding{AssistantID: id, Tools: []string{}}
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
	return b, len(b.Tools) > 0, nil
}
