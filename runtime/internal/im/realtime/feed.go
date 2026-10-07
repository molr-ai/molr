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
	Authorizer   policy.Authorizer
	PollInterval time.Duration
}
type feed struct {
	store   Store
	options Options
}

// NewFeed fails closed unless a current-principal authorizer is configured.
// Polling replays committed rows and checks revocation even on an idle stream.
func NewFeed(store Store, options Options) Feed {
	if options.PollInterval <= 0 {
		options.PollInterval = 100 * time.Millisecond
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
	// Replay validates owner/query-bound cursors now, rather than after subscribing.
	if _, err := f.store.Replay(ctx, p.OwnerID, q); err != nil {
		return nil, err
	}
	topics, err := normalizedTopics(q.Topics)
	if err != nil {
		return nil, err
	}
	q.Topics = topics
	subscriptionCtx, cancel := context.WithCancel(ctx)
	return &subscription{feed: f, principal: p, query: q, ctx: subscriptionCtx, cancel: cancel}, nil
}

type subscription struct {
	feed      *feed
	principal core.Principal
	query     Query
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
}

func (s *subscription) Close() error { s.cancel(); return nil }
func (s *subscription) Next(ctx context.Context) (Visible, error) {
	// A subscription is a sequential iterator. Close never needs this mutex.
	s.mu.Lock()
	defer s.mu.Unlock()
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	for {
		if err := persistence.CheckContext(s.ctx); err != nil {
			return Visible{}, err
		}
		if err := s.feed.authorize(callCtx, s.principal); err != nil {
			return Visible{}, err
		}
		q := s.query
		q.Page.Limit = 1
		page, err := s.feed.store.Replay(callCtx, s.principal.OwnerID, q)
		if err != nil {
			return Visible{}, err
		}
		if len(page.Items) > 0 {
			// Do not hand over even buffered data after observed revocation.
			if err := s.feed.authorize(callCtx, s.principal); err != nil {
				return Visible{}, err
			}
			e := page.Items[0]
			s.query.After = encodeCursor(s.principal.OwnerID, s.query.Topics, e.Sequence)
			s.query.Page.Cursor = ""
			return e, nil
		}
		timer := time.NewTimer(s.feed.options.PollInterval)
		select {
		case <-callCtx.Done():
			timer.Stop()
			return Visible{}, persistence.CheckContext(callCtx)
		case <-timer.C:
		}
	}
}
