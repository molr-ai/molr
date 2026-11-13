package scheduler

import (
	"context"
	"molr.ai/runtime/internal/event"
	"molr.ai/runtime/internal/persistence"
)

// Subscribe before checking the claim so a transactional cancellation cannot be
// lost between claiming work and entering the handler. No idle reads or timers.
func (s *service) watchClaim(ctx context.Context, claim Claim, cancel context.CancelFunc) (func(), error) {
	key := persistence.Key(claim.Job.Event.OwnerID, claim.Job.Event.ID)
	changes, err := s.store.Watch(persistence.RecordSignal(jobsBucket, key))
	if err != nil {
		return nil, err
	}
	check := func() bool {
		current, err := persistence.Load[Job](ctx, s.store, jobsBucket, key)
		if err != nil || current.State != event.Running || current.ExecutionID != claim.Job.ExecutionID {
			cancel()
			return false
		}
		return true
	}
	if !check() {
		changes.Close()
		return func() {}, nil
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-changes.Done():
				cancel()
				return
			case <-changes.Changes():
				if !check() {
					return
				}
			}
		}
	}()
	return func() { close(done); changes.Close(); <-stopped }, nil
}
