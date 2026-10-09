package agent

import (
	"context"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

// Activity is the small execution projection needed by the assistant list.
type Activity struct {
	AssistantID core.ID
	Dreaming    bool
}

// Activities reads at most one page of explicitly authorized assistants. It
// avoids loading checkpoints, interruption boundaries or expedite details.
func (e *Engine) Activities(ctx context.Context, p core.Principal, ids []core.ID) ([]Activity, error) {
	if err := p.RequireOwner(); err != nil {
		return nil, err
	}
	if len(ids) > 200 {
		return nil, core.Invalid("assistant_ids", "at most 200 assistants required")
	}
	result := make([]Activity, 0, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		if err := id.Validate(); err != nil {
			return nil, err
		}
		if err := e.authorize(ctx, p, id, policy.Read); err != nil {
			return nil, err
		}
		keys = append(keys, persistence.Key(p.OwnerID, id))
	}
	statuses, err := persistence.Select[Status](ctx, e.store, statusBucket, persistence.Query{
		Where:  []persistence.Filter{{Field: "key", Op: "in", Value: keys}},
		Fields: []string{"AssistantID", "State", "DreamDeadline"}, Limit: len(ids),
	})
	if err != nil {
		return nil, err
	}
	available := e.options.CheckEnvironment == nil || e.options.CheckEnvironment(ctx) == nil
	for _, status := range statuses {
		result = append(result, Activity{AssistantID: status.AssistantID, Dreaming: available && status.State == Dreaming && (status.DreamDeadline == nil || e.options.Now().Before(*status.DreamDeadline))})
	}
	return result, nil
}
