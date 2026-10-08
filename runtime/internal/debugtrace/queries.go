package debugtrace

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"slices"
)

// Protected history consists only of active runs and the newest MinRuns completed
// runs. Inspecting one capture must not load every older capture for that owner.
func (s *Service) protected(ctx context.Context, store persistence.Store, owner core.ID, policy Retention) (map[core.ID]bool, error) {
	filters := []persistence.Filter{persistence.Eq("OwnerID", owner), {Field: "Trigger", Op: "!=", Value: "dream_due"}}
	active, err := persistence.Select[Run](ctx, store, "debug.summaries", persistence.Query{Where: append(append([]persistence.Filter{}, filters...), persistence.Eq("State", "running")), Fields: []string{"ID"}})
	if err != nil {
		return nil, err
	}
	out := map[core.ID]bool{}
	for _, run := range active {
		out[run.ID] = true
	}
	if policy.MinRuns > 0 {
		completed, err := persistence.Select[Run](ctx, store, "debug.summaries", persistence.Query{Where: append(filters, persistence.Filter{Field: "State", Op: "!=", Value: "running"}), OrderBy: "StartedAtKey", Descending: true, ThenBy: "FirstRevision", ThenDescending: true, Limit: policy.MinRuns, Fields: []string{"ID"}})
		if err != nil {
			return nil, err
		}
		for _, run := range completed {
			out[run.ID] = true
		}
	}
	return out, nil
}
func visibleQuery(owner core.ID, protected map[core.ID]bool, cutoff string) persistence.Query {
	q := persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner)}, Any: [][]persistence.Filter{{persistence.Eq("Trigger", "dream_due")}, {{Field: "StartedAtKey", Op: ">=", Value: cutoff}}, {persistence.Eq("State", "running")}}}
	ids := []core.ID{}
	for id := range protected {
		ids = append(ids, id)
	}
	// Stable membership order is required for query-bound pagination cursors.
	slices.Sort(ids)
	if len(ids) > 0 {
		q.Any = append(q.Any, []persistence.Filter{{Field: "ID", Op: "in", Value: ids}})
	}
	return q
}
