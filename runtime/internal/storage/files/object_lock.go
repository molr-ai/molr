package files

import (
	"context"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// Streaming and publication of different objects are independent. References
// include waiters, so a lock cannot be replaced while another operation uses it.
// The registry retains no entry once the last operation releases its reference.
type objectLock struct {
	token chan struct{}
	refs  int
}

func (o *ObjectStore) lock(ctx context.Context, id core.ID) (func(), error) {
	o.mu.Lock()
	if o.locks == nil {
		o.locks = map[core.ID]*objectLock{}
	}
	lock := o.locks[id]
	if lock == nil {
		lock = &objectLock{token: make(chan struct{}, 1)}
		lock.token <- struct{}{}
		o.locks[id] = lock
	}
	lock.refs++
	o.mu.Unlock()
	releaseReference := func() {
		o.mu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(o.locks, id)
		}
		o.mu.Unlock()
	}
	select {
	case <-lock.token:
		release := func() { lock.token <- struct{}{}; releaseReference() }
		if err := persistence.CheckContext(ctx); err != nil {
			release()
			return nil, err
		}
		return release, nil
	case <-ctx.Done():
		releaseReference()
		return nil, persistence.CheckContext(ctx)
	}
}
