package sqlitestore

import (
	"context"
	"strings"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type watch struct {
	store   *Store
	key     persistence.SignalKey
	changes chan struct{}
	done    chan struct{}
}

func validSignal(key persistence.SignalKey) error {
	if key.Namespace == "" || len(key.Namespace) > 128 || key.Key == "" || len(key.Key) > 4096 || strings.ContainsRune(key.Namespace+key.Key, 0) {
		return core.Invalid("signal", "invalid namespace or key")
	}
	return nil
}

func (s *Store) Watch(key persistence.SignalKey) (persistence.Watch, error) {
	if err := validSignal(key); err != nil {
		return nil, err
	}
	s.wakeMu.Lock()
	defer s.wakeMu.Unlock()
	if s.closed.Load() {
		return nil, core.NewError(core.Unavailable, "database closed")
	}
	w := &watch{store: s, key: key, changes: make(chan struct{}, 1), done: make(chan struct{})}
	if s.watches == nil {
		s.watches = make(map[persistence.SignalKey]map[*watch]struct{})
	}
	if s.watches[key] == nil {
		s.watches[key] = make(map[*watch]struct{})
	}
	s.watches[key][w] = struct{}{}
	return w, nil
}
func (b boundStore) Watch(key persistence.SignalKey) (persistence.Watch, error) {
	if err := b.tx.check(b.tx.ctx); err != nil {
		return nil, err
	}
	return b.tx.store.Watch(key)
}
func (w *watch) Changes() <-chan struct{} { return w.changes }
func (w *watch) Done() <-chan struct{}    { return w.done }
func (w *watch) Close() {
	w.store.wakeMu.Lock()
	defer w.store.wakeMu.Unlock()
	if _, ok := w.store.watches[w.key][w]; !ok {
		return
	}
	delete(w.store.watches[w.key], w)
	if len(w.store.watches[w.key]) == 0 {
		delete(w.store.watches, w.key)
	}
	close(w.done)
}
func (s *Store) closeWatches() {
	s.wakeMu.Lock()
	defer s.wakeMu.Unlock()
	for _, group := range s.watches {
		for w := range group {
			close(w.done)
		}
	}
	s.watches = nil
}
func (s *Store) publish(keys []persistence.SignalKey) {
	if len(keys) == 0 {
		return
	}
	s.wakeMu.Lock()
	defer s.wakeMu.Unlock()
	seen := make(map[persistence.SignalKey]bool, len(keys))
	for _, key := range keys {
		if seen[key] {
			continue
		}
		seen[key] = true
		for w := range s.watches[key] {
			select {
			case w.changes <- struct{}{}:
			default:
			}
		}
	}
}
func (tx *transaction) NotifyAfterCommit(ctx context.Context, key persistence.SignalKey) error {
	if err := tx.check(ctx); err != nil {
		return err
	}
	if !tx.writable {
		return core.NewError(core.FailedPrecondition, "read-only transaction")
	}
	if err := validSignal(key); err != nil {
		return err
	}
	tx.signals = append(tx.signals, key)
	return nil
}
