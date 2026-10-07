package realtime

import (
	"context"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

type contractFixture struct {
	Feed         Feed
	Store        Store
	Owner, Other core.Principal
	// Revoke mutates the backing session/token authority used by Feed.
	Revoke func(core.Principal)
}

var newContractFixture func(*testing.T) contractFixture

func visible(owner core.ID, id core.ID) Visible {
	return Visible{ID: id, OwnerID: owner, Topic: "message.created", Object: core.Object{Kind: "message", ID: id}, Version: 1, CreatedAt: time.Now()}
}
func TestTDDRealtimeReplayIsOwnerScopedAndDoesNotConsume(t *testing.T) {
	f := testkit.RequireFactory(t, "im/realtime", newContractFixture)
	ctx := testkit.Context(t)
	for _, e := range []Visible{visible(f.Owner.OwnerID, "one"), visible(f.Owner.OwnerID, "two"), visible(f.Other.OwnerID, "foreign")} {
		testkit.RequireNoError(t, f.Store.Append(ctx, e))
	}
	first, err := f.Feed.List(ctx, f.Owner, Query{Page: core.PageRequest{Limit: 1}})
	testkit.RequireNoError(t, err)
	if len(first.Items) != 1 || first.Items[0].OwnerID != f.Owner.OwnerID || first.NextCursor == "" {
		t.Fatalf("first page = %+v", first)
	}
	second, err := f.Feed.List(ctx, f.Owner, Query{After: first.NextCursor, Page: core.PageRequest{Limit: 1}})
	testkit.RequireNoError(t, err)
	if len(second.Items) != 1 || second.Items[0].OwnerID != f.Owner.OwnerID || second.Items[0].Sequence <= first.Items[0].Sequence {
		t.Fatalf("next page = %+v", second)
	}
	for i := 0; i < 2; i++ {
		p, err := f.Store.Replay(ctx, f.Owner.OwnerID, Query{})
		testkit.RequireNoError(t, err)
		if len(p.Items) != 2 {
			t.Fatalf("replay consumed events: %+v", p)
		}
	}
	_, err = f.Feed.List(ctx, f.Other, Query{After: first.NextCursor})
	if err == nil {
		t.Fatal("foreign owner's cursor accepted")
	}
	filtered, err := f.Feed.List(ctx, f.Owner, Query{Topics: []string{"task.updated"}})
	testkit.RequireNoError(t, err)
	if len(filtered.Items) != 0 {
		t.Fatal("topic filter ignored")
	}
}
func TestTDDRealtimeEverySubscriberReceivesLiveEventAndRevocationStopsDelivery(t *testing.T) {
	f := testkit.RequireFactory(t, "im/realtime", newContractFixture)
	ctx := testkit.Context(t)
	a, err := f.Feed.Subscribe(ctx, f.Owner, Query{})
	testkit.RequireNoError(t, err)
	defer a.Close()
	b, err := f.Feed.Subscribe(ctx, f.Owner, Query{})
	testkit.RequireNoError(t, err)
	defer b.Close()
	testkit.RequireNoError(t, f.Store.Append(ctx, visible(f.Owner.OwnerID, "live")))
	for _, s := range []Subscription{a, b} {
		e, err := s.Next(ctx)
		testkit.RequireNoError(t, err)
		if e.ID != "live" || e.OwnerID != f.Owner.OwnerID {
			t.Fatalf("live event = %+v", e)
		}
	}
	f.Revoke(f.Owner)
	_, err = a.Next(ctx)
	testkit.RequireCode(t, err, core.Unauthenticated)
	_, err = f.Feed.Subscribe(ctx, f.Owner, Query{})
	testkit.RequireCode(t, err, core.Unauthenticated)
	_, err = f.Feed.List(ctx, f.Owner, Query{})
	testkit.RequireCode(t, err, core.Unauthenticated)
}
func TestTDDRealtimeSubscriptionCancellationAndClose(t *testing.T) {
	f := testkit.RequireFactory(t, "im/realtime", newContractFixture)
	ctx := testkit.Context(t)
	s, err := f.Feed.Subscribe(ctx, f.Owner, Query{})
	testkit.RequireNoError(t, err)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = s.Next(canceled)
	testkit.RequireCode(t, err, core.Canceled)
	testkit.RequireNoError(t, s.Close())
	_, err = s.Next(ctx)
	if err == nil {
		t.Fatal("closed subscription still delivers")
	}
	testkit.RequireNoError(t, s.Close())
}
