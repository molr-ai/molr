// Package resultcache caches immutable function results within one Runtime.
// Callers own key construction, authorization and copying mutable values.
package resultcache

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"
)

var ErrClosed = errors.New("result cache closed")
var ErrBusy = errors.New("too many cache computations")
var ErrPanic = errors.New("cache computation panicked")

type Options[V any] struct {
	MaxEntries  int
	MaxCost     int64
	LoadTimeout time.Duration
	Cost        func(V) int64
}

type Result[V any] struct {
	Value     V
	WrittenAt time.Time
	Hit       bool
	Stored    bool
}

type entry[K comparable, V any] struct {
	key     K
	result  Result[V]
	expires time.Time
	cost    int64
}

type flight[V any] struct {
	done     chan struct{}
	cancel   context.CancelFunc
	waiters  int
	finished bool
	result   Result[V]
	err      error
}

type Cache[K comparable, V any] struct {
	mu      sync.Mutex
	options Options[V]
	now     func() time.Time
	entries map[K]*list.Element
	order   *list.List
	cost    int64
	flights map[K]*flight[V]
	closed  bool
}

// New requires positive entry/cost bounds, a load timeout and a cost function.
// Cost measures retained values; callers should account for their payload size.
func New[K comparable, V any](options Options[V]) *Cache[K, V] {
	if options.MaxEntries <= 0 || options.MaxCost <= 0 || options.LoadTimeout <= 0 || options.Cost == nil {
		panic("invalid result cache options")
	}
	return &Cache[K, V]{options: options, now: time.Now, entries: make(map[K]*list.Element), order: list.New(), flights: make(map[K]*flight[V])}
}

// GetOrCompute uses a fixed TTL from successful insertion; reads never renew it.
// Refresh bypasses stored results but joins an already running computation.
// A canceled waiter does not cancel other waiters. Once all waiters leave, the
// computation is canceled and cannot overwrite a later result for the same key.
// The loader must honor its context and return values that callers won't mutate.
func (c *Cache[K, V]) GetOrCompute(ctx context.Context, key K, ttl time.Duration, refresh bool, load func(context.Context) (V, error)) (Result[V], error) {
	var zero Result[V]
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return zero, ErrClosed
	}
	if ttl <= 0 {
		c.mu.Unlock()
		value, err := load(ctx)
		return Result[V]{Value: value}, err
	}
	if item := c.entries[key]; item != nil {
		e := item.Value.(*entry[K, V])
		if !c.now().Before(e.expires) {
			c.remove(item)
		} else if !refresh {
			c.order.MoveToFront(item)
			result := e.result
			result.Hit = true
			c.mu.Unlock()
			return result, nil
		}
	}
	f := c.flights[key]
	if f == nil {
		if len(c.flights) >= c.options.MaxEntries {
			c.mu.Unlock()
			return zero, ErrBusy
		}
		// Keep request values (including authorization identity), but give the shared
		// computation its own bounded lifetime instead of the first caller's cancel.
		work, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.options.LoadTimeout)
		f = &flight[V]{done: make(chan struct{}), cancel: cancel}
		c.flights[key] = f
		go c.compute(work, key, ttl, f, load)
	}
	f.waiters++
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		c.mu.Lock()
		if !f.finished {
			f.waiters--
			if f.waiters == 0 {
				if c.flights[key] == f {
					delete(c.flights, key)
				}
				f.cancel()
			}
		}
		c.mu.Unlock()
		return zero, ctx.Err()
	case <-f.done:
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		return f.result, f.err
	}
}

func (c *Cache[K, V]) compute(ctx context.Context, key K, ttl time.Duration, f *flight[V], load func(context.Context) (V, error)) {
	var value V
	var err error
	var cost int64
	func() {
		defer func() {
			if recover() != nil {
				err = ErrPanic
			}
		}()
		value, err = load(ctx)
		if err == nil {
			cost = c.options.Cost(value)
		}
	}()
	if err == nil {
		err = ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	defer f.cancel()
	if f.finished {
		return
	}
	f.result = Result[V]{Value: value}
	f.err = err
	if c.flights[key] == f {
		delete(c.flights, key)
		if !c.closed && err == nil && f.waiters > 0 {
			// A successful oversized refresh must not leave the old value reusable.
			if item := c.entries[key]; item != nil {
				c.remove(item)
			}
		}
		if !c.closed && err == nil && f.waiters > 0 && cost >= 0 && cost <= c.options.MaxCost {
			now := c.now()
			for _, item := range c.entries {
				if !now.Before(item.Value.(*entry[K, V]).expires) {
					c.remove(item)
				}
			}
			for len(c.entries) >= c.options.MaxEntries || c.cost > c.options.MaxCost-cost {
				c.remove(c.order.Back())
			}
			f.result.WrittenAt = now
			f.result.Stored = true
			item := c.order.PushFront(&entry[K, V]{key: key, result: f.result, expires: now.Add(ttl), cost: cost})
			c.entries[key] = item
			c.cost += cost
		}
	}
	f.finished = true
	close(f.done)
}

func (c *Cache[K, V]) remove(item *list.Element) {
	e := item.Value.(*entry[K, V])
	delete(c.entries, e.key)
	c.cost -= e.cost
	c.order.Remove(item)
}

// Close releases retained values, cancels outstanding work and wakes waiters.
// It is safe to call repeatedly; loaders must honor cancellation to finish.
func (c *Cache[K, V]) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	clear(c.entries)
	c.order.Init()
	c.cost = 0
	for key, f := range c.flights {
		f.cancel()
		f.err = ErrClosed
		f.finished = true
		close(f.done)
		delete(c.flights, key)
	}
}
