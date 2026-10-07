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
		body := v.Text
		if len(body) > 8<<10 {
			body = "Summary exceeds the automatic context budget; read " + ref.LogicalPath() + " explicitly with file_read."
		}
		out = append(out, agent.ContextDocument{Kind: "memory", Source: ref.LogicalPath(), Scope: string(ref.Scope), Revision: v.Version, Content: body})
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
