package feishu

import (
	"context"
	"molr.ai/runtime/internal/persistence"
)

// Single-step adapter for existing protocol fixtures; dispatch uses the same
// binding-scoped production query as the event-driven worker.
func (s *Service) deliverNext(ctx context.Context) error {
	rows, err := persistence.Scan[Binding](ctx, s.store, bindingsBucket)
	if err != nil {
		return err
	}
	for _, b := range rows {
		if s.options.Live(ctx, b.OwnerID, b.AssistantID) != nil {
			continue
		}
		_, err = s.deliverBinding(ctx, b)
		if err != nil && ctx.Err() != nil {
			return err
		}
	}
	return nil
}
