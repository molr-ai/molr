package app

import (
	"context"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
)

func runtimeMemoryRoots(ctx context.Context, store persistence.Store, dataDir, workRoot string, owner core.ID, q memory.RootQuery) ([]memory.Root, error) {
	roots := []memory.Root{}
	if q.Scope != "" && q.Scope != memory.Global && q.Scope != memory.Assistant {
		return roots, nil
	}
	if (q.Scope == "" || q.Scope == memory.Global) && q.AssistantID == "" {
		roots = append(roots, memory.Root{Scope: memory.Global, Directory: platform.UserGlobal(dataDir, owner)})
	}
	if q.Scope == memory.Global {
		return roots, nil
	}
	query := persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner)}, Fields: []string{"ID", "OwnerID"}}
	if q.AssistantID != "" {
		query.Where = append(query.Where, persistence.Eq("key", persistence.Key(owner, q.AssistantID)))
		query.Limit = 1
	}
	// Resolving paths needs identity only, never names, avatars or other profiles.
	rows, err := persistence.Select[struct{ ID, OwnerID core.ID }](ctx, store, "business.assistants", query)
	if err != nil {
		return nil, err
	}
	for _, a := range rows {
		if a.OwnerID == owner && (q.AssistantID == "" || a.ID == q.AssistantID) {
			roots = append(roots, memory.Root{Scope: memory.Assistant, AssistantID: a.ID, Directory: platform.AssistantWorkspace(dataDir, workRoot, owner, a.ID)})
		}
	}
	return roots, nil
}
