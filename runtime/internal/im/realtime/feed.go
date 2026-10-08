package realtime

import (
	"context"
	"sync"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

type Options struct {
	Authorizer            policy.Authorizer
	AuthorizationInterval time.Duration
}
type feed struct {
	store   Store
	options Options
}

// NewFeed fails closed unless a current-principal authorizer is configured.
// Committed writes wake subscribers. Only authorization is checked periodically.
func NewFeed(store Store, options Options) Feed {
	if options.AuthorizationInterval <= 0 {
		options.AuthorizationInterval = 100 * time.Millisecond
	}
	return &feed{store: store, options: options}
}
func (f *feed) authorize(ctx context.Context, p core.Principal) error {
	if err := persistence.CheckContext(ctx); err != nil {
		return err
	}
	if err := p.RequireOwner(); err != nil {
		return err
	}
	if f.options.Authorizer == nil {
		return core.NewError(core.Unauthenticated, "authentication unavailable")
	}
	return f.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "realtime", ID: p.OwnerID}, Action: policy.Read})
}
func (f *feed) List(ctx context.Context, p core.Principal, q Query) (core.Page[Visible], error) {
	if err := f.authorize(ctx, p); err != nil {
		return core.Page[Visible]{}, err
	}
	return f.store.Replay(ctx, p.OwnerID, q)
}
func (f *feed) Subscribe(ctx context.Context, p core.Principal, q Query) (Subscription, error) {
	if err := f.authorize(ctx, p); err != nil {
		return nil, err
	}
	q, _, err := normalizeQuery(p.OwnerID, q)
	if err != nil {
		return nil, err
	}
	// Register before the initial read so a commit cannot fall between reading
	// the tail and beginning to wait. All Store wrappers share this watch hub.
	watch, err := f.store.Watch(p.OwnerID)
	if err != nil {
		return nil, err
	}
	subscriptionCtx, cancel := context.WithCancel(ctx)
	stopWatch := context.AfterFunc(subscriptionCtx, watch.Close)
	return &subscription{feed: f, principal: p, query: q, ctx: subscriptionCtx, cancel: cancel, watch: watch, stopWatch: stopWatch}, nil
}

type subscription struct {
	feed      *feed
	principal core.Principal
	query     Query
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	watch     persistence.Watch
	stopWatch func() bool
	buffer    []Visible
}

func (s *subscription) Close() error {
	s.cancel()
	s.stopWatch()
	s.watch.Close()
	return nil
}
func (s *subscription) check(ctx context.Context) error {
	if err := persistence.CheckContext(s.ctx); err != nil {
		return err
	}
	if err := persistence.CheckContext(ctx); err != nil {
		return err
	}
	select {
	case <-s.watch.Done():
		return core.NewError(core.Unavailable, "event store closed")
	default:
	}
	return s.feed.authorize(ctx, s.principal)
}
func (s *subscription) Next(ctx context.Context) (Visible, error) {
	// Close never takes this mutex, so it can interrupt a waiting Next.
	s.mu.Lock()
	defer s.mu.Unlock()
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	if err := s.check(callCtx); err != nil {
		return Visible{}, err
	}
	for len(s.buffer) == 0 {
		// Consume only BEFORE reading. A signal arriving during/after the query
		// remains pending and prevents sleeping past a newly committed event.
		select {
		case <-s.watch.Changes():
		default:
		}
		q := s.query
		q.Page.Limit = 100
		page, err := s.feed.store.Replay(callCtx, s.principal.OwnerID, q)
		if err != nil {
			return Visible{}, err
		}
		s.buffer = page.Items
		if len(s.buffer) > 0 {
			break
		}
		if err := s.wait(callCtx); err != nil {
			return Visible{}, err
		}
	}
	// Revalidate even preloaded events after a slow consumer or revocation.
	if err := s.check(callCtx); err != nil {
		return Visible{}, err
	}
	e := s.buffer[0]
	s.buffer[0] = Visible{}
	s.buffer = s.buffer[1:]
	s.query.After = encodeCursor(s.principal.OwnerID, s.query.Topics, e.Sequence)
	s.query.Page.Cursor = ""
	return e, nil
}
func (s *subscription) wait(ctx context.Context) error {
	ticker := time.NewTicker(s.feed.options.AuthorizationInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return persistence.CheckContext(ctx)
		case <-s.watch.Done():
			if err := persistence.CheckContext(s.ctx); err != nil {
				return err
			}
			return core.NewError(core.Unavailable, "event store closed")
		case <-s.watch.Changes():
			return s.check(ctx)
		case <-ticker.C:
			// Authorization checks never trigger an event replay. There is no data
			// polling timer; reconnects recover from durable, owner-bound cursors.
			if err := s.check(ctx); err != nil {
				return err
			}
		}
	}
}
