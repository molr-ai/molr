package slack

import (
	"context"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// channelGate serializes binding changes and sends while allowing waiting
// requests to end without disturbing the current holder.
type channelGate chan struct{}

func (g channelGate) acquire(ctx context.Context) error {
	if err := persistence.CheckContext(ctx); err != nil {
		return err
	}
	select {
	case g <- struct{}{}:
		if err := persistence.CheckContext(ctx); err != nil {
			g.release()
			return err
		}
		return nil
	case <-ctx.Done():
		return persistence.CheckContext(ctx)
	}
}
func (g channelGate) release() { <-g }

func (s *Service) gate(owner, id core.ID) channelGate {
	key := persistence.Key(owner, id)
	if value, ok := s.gates.Load(key); ok {
		return value.(channelGate)
	}
	value, _ := s.gates.LoadOrStore(key, make(channelGate, 1))
	return value.(channelGate)
}
