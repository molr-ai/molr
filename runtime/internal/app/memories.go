package app

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type memoryAccess struct {
	memories memory.Service
	store    persistence.Store
}

// Only the two explicit summaries are injected. Detailed content is discovered
// through ordinary file_search/file_read and never implicitly ranked into context.
func (m memoryAccess) context(ctx context.Context, r agent.ContextRequest) ([]agent.ContextDocument, error) {
	out := []agent.ContextDocument{}
	for _, ref := range []memory.Ref{{Scope: memory.Global, Path: "MEMORY.md"}, {Scope: memory.Assistant, AssistantID: r.Execution.AssistantID, Path: "MEMORY.md"}} {
		v, err := m.memories.Get(ctx, r.Execution.Principal, ref)
		if core.CodeOf(err) == core.NotFound {
			continue
		}
		if err != nil {
			return nil, err
		}
		if v.State != memory.Active {
			continue
		}
		valid := true
		for _, dependency := range v.Dependencies {
			topic, err := m.memories.Get(ctx, r.Execution.Principal, dependency.Ref)
			if core.CodeOf(err) == core.NotFound {
				valid = false
				break
			}
			if err != nil {
				return nil, err
			}
			if topic.Version != dependency.Version || topic.State != memory.Active {
				valid = false
				break
			}
		}
		body := v.Text
		if !valid {
			body = "Core cognition is stale because a supporting topic changed or disappeared. Do not reuse earlier summary facts. Read current topic files when needed."
		}
		if len(body) > 8<<10 {
			body = "Summary exceeds the automatic context budget; read " + ref.LogicalPath() + " explicitly with file_read."
		}
		out = append(out, agent.ContextDocument{Kind: "memory", Source: ref.LogicalPath(), Scope: string(ref.Scope), Revision: sourceRevision(v.Version + body), Content: body})
	}
	prefs, err := settings.NewRepository(m.store).Get(ctx, r.Execution.Principal.OwnerID)
	if err != nil && !persistence.Missing(err) {
		return nil, err
	}
	if prefs.AnswerPreferences != "" {
		out = append(out, agent.ContextDocument{Kind: "memory", Source: "user_answer_preferences", Scope: "owner", Revision: fmt.Sprint(prefs.Version), Content: prefs.AnswerPreferences})
	}
	return out, nil
}
