package realtime

import (
	"context"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

func TestNewSubscriptionStartsAtHeadAndReconnectReplaysGap(t *testing.T) {
	f := newContractFixture(t)
	testkit.RequireNoError(t, f.Store.Append(t.Context(), visible(f.Owner.OwnerID, "history")))
	sub, err := f.Feed.Subscribe(t.Context(), f.Owner, Query{})
	testkit.RequireNoError(t, err)
	defer sub.Close()
	position := sub.StartCursor()
	seq, err := parseCursor(position, f.Owner.OwnerID, []string{})
	testkit.RequireNoError(t, err)
	if seq != 1 {
		t.Fatal(seq)
	}
	testkit.RequireNoError(t, f.Store.Append(t.Context(), visible(f.Owner.OwnerID, "after-subscribe")))
	event, err := sub.Next(t.Context())
	testkit.RequireNoError(t, err)
	if event.ID != "after-subscribe" {
		t.Fatal(event)
	}
	// The handshake position remains usable even if the first update is lost.
	resumed, err := f.Feed.Subscribe(t.Context(), f.Owner, Query{After: position})
	testkit.RequireNoError(t, err)
	defer resumed.Close()
	event, err = resumed.Next(t.Context())
	testkit.RequireNoError(t, err)
	if event.ID != "after-subscribe" {
		t.Fatal(event)
	}
	page, err := f.Feed.List(t.Context(), f.Owner, Query{})
	testkit.RequireNoError(t, err)
	if len(page.Items) != 2 {
		t.Fatal("explicit history no longer replays", page)
	}
}

type headRaceStore struct {
	Store
	afterHead func()
}

func (s headRaceStore) Head(ctx context.Context, owner core.ID, topics []string) (string, error) {
	head, err := s.Store.Head(ctx, owner, topics)
	s.afterHead()
	return head, err
}

func TestCommitDuringInitialSubscriptionIsDelivered(t *testing.T) {
	_, store, owner := newWakeupStore(t)
	race := headRaceStore{Store: store, afterHead: func() {
		testkit.RequireNoError(t, store.Append(t.Context(), visible(owner.OwnerID, "during-subscribe")))
	}}
	sub, err := NewFeed(race, Options{Authorizer: authorizerFunc(allow)}).Subscribe(t.Context(), owner, Query{})
	testkit.RequireNoError(t, err)
	defer sub.Close()
	if sub.StartCursor() == "" {
		t.Fatal("empty streams need a resumable position")
	}
	event, err := sub.Next(t.Context())
	testkit.RequireNoError(t, err)
	if event.ID != "during-subscribe" {
		t.Fatal(event)
	}
}
