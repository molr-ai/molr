package sqlitestore

import (
	"context"
	"strings"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type watch struct {
	store   *Store
	keys    []persistence.SignalKey
	changes chan struct{}
	done    chan struct{}
}

func validSignal(key persistence.SignalKey) error {
	if key.Namespace == "" || len(key.Namespace) > 128 || key.Key == "" || len(key.Key) > 4096 || strings.ContainsRune(key.Namespace+key.Key, 0) {
		return core.Invalid("signal", "invalid namespace or key")
	}
	return nil
}

func (s *Store) Watch(keys ...persistence.SignalKey) (persistence.Watch, error) {
	if len(keys) == 0 {
		return nil, core.Invalid("signal", "at least one signal required")
	}
	for _, key := range keys {
		if err := validSignal(key); err != nil {
			return nil, err
		}
	}
	s.wakeMu.Lock()
	defer s.wakeMu.Unlock()
	if s.closed.Load() {
		return nil, core.NewError(core.Unavailable, "database closed")
	}
	w := &watch{store: s, keys: append([]persistence.SignalKey(nil), keys...), changes: make(chan struct{}, 1), done: make(chan struct{})}
	if s.watches == nil {
		s.watches = make(map[persistence.SignalKey]map[*watch]struct{})
	}
	for _, key := range keys {
		if s.watches[key] == nil {
			s.watches[key] = make(map[*watch]struct{})
		}
		s.watches[key][w] = struct{}{}
	}
	return w, nil
}
func (b boundStore) Watch(keys ...persistence.SignalKey) (persistence.Watch, error) {
	if err := b.tx.check(b.tx.ctx); err != nil {
		return nil, err
	}
	return b.tx.store.Watch(keys...)
}
func (w *watch) Changes() <-chan struct{} { return w.changes }
func (w *watch) Done() <-chan struct{}    { return w.done }
func (w *watch) Close() {
	w.store.wakeMu.Lock()
	defer w.store.wakeMu.Unlock()
	select {
	case <-w.done:
		return
	default:
	}
	for _, key := range w.keys {
		delete(w.store.watches[key], w)
		if len(w.store.watches[key]) == 0 {
			delete(w.store.watches, key)
		}
	}
	close(w.done)
}
func (s *Store) closeWatches() {
	s.wakeMu.Lock()
	defer s.wakeMu.Unlock()
	for _, group := range s.watches {
		for w := range group {
			select {
			case <-w.done:
			default:
				close(w.done)
			}
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

func (tx *transaction) changed(bucket, key string) {
	tx.signals = append(tx.signals, persistence.BucketSignal(bucket))
	if key != "" {
		tx.signals = append(tx.signals, persistence.RecordSignal(bucket, key))
	}
}
