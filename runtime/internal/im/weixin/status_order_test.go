package weixin

import (
	"context"
	"fmt"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

func TestStatusKeepsNewestDeliveriesWithinOneMillisecond(t *testing.T) {
	f := setup(t)
	binding := f.bind(t)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	must(t, f.db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < 100; i++ {
			id := core.ID(fmt.Sprintf("delivery-%03d", i))
			d := Delivery{ID: id, OwnerID: "owner", BindingID: binding.ID, AssistantID: "assistant", ConversationID: "chat", MessageID: core.ID(fmt.Sprintf("message-%03d", i)), Sequence: uint64(i + 1), State: "accepted", CreatedAt: start.Add(time.Duration(i) * time.Nanosecond)}
			if err := tx.Put(ctx, deliveriesBucket, persistence.Key("owner", id), d); err != nil {
				return err
			}
		}
		return nil
	}))
	status, err := f.s.Status(t.Context(), f.owner, "assistant")
	must(t, err)
	if len(status.Deliveries) != 30 {
		t.Fatalf("got %d deliveries, want 30", len(status.Deliveries))
	}
	t.Logf("returned delivery sequence range %d..%d, want 71..100", status.Deliveries[0].Sequence, status.Deliveries[29].Sequence)
	for i, d := range status.Deliveries {
		if d.Sequence != uint64(71+i) {
			t.Fatalf("position %d has sequence %d, want %d", i, d.Sequence, 71+i)
		}
	}
}

func TestStatusOrderingKeepsBindingScopeAndStableTies(t *testing.T) {
	for _, sameInstant := range []bool{false, true} {
		t.Run(fmt.Sprintf("same_instant=%v", sameInstant), func(t *testing.T) {
			f := setup(t)
			binding := f.bind(t)
			start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
			must(t, f.db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
				for i := 0; i < 100; i++ {
					at := start
					if !sameInstant {
						at = at.Add(time.Duration(i) * time.Nanosecond)
					}
					if i%2 == 0 {
						at = at.In(time.FixedZone("offset", 8*3600))
					}
					id := core.ID(fmt.Sprintf("delivery-%03d", i))
					d := Delivery{ID: id, OwnerID: "owner", BindingID: binding.ID, Sequence: uint64(i/2 + 1), Part: i % 2, State: "unknown", Error: "synthetic_error", Attempts: 3, CreatedAt: at}
					if err := tx.Put(ctx, deliveriesBucket, persistence.Key("owner", id), d); err != nil {
						return err
					}
				}
				for i, scope := range []struct{ owner, binding core.ID }{{"other", binding.ID}, {"owner", "other-binding"}, {"owner-extra", binding.ID}} {
					id := core.ID(fmt.Sprintf("unrelated-%d", i))
					d := Delivery{ID: id, OwnerID: scope.owner, BindingID: scope.binding, Sequence: 999, CreatedAt: start.Add(time.Hour)}
					if err := tx.Put(ctx, deliveriesBucket, persistence.Key(scope.owner, id), d); err != nil {
						return err
					}
				}
				return nil
			}))
			for range 2 {
				status, err := f.s.Status(t.Context(), f.owner, "assistant")
				must(t, err)
				if len(status.Deliveries) != 30 {
					t.Fatal(len(status.Deliveries))
				}
				for i, d := range status.Deliveries {
					n := 70 + i
					if d.ID != core.ID(fmt.Sprintf("delivery-%03d", n)) || d.Sequence != uint64(n/2+1) || d.Part != n%2 || d.State != "unknown" || d.Error != "synthetic_error" || d.Attempts != 3 {
						t.Fatalf("unexpected delivery %d: %+v", i, d)
					}
				}
			}
		})
	}
}

func TestStatusSelectsCreationTimeBeforeSequencePresentation(t *testing.T) {
	f := setup(t)
	binding := f.bind(t)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	must(t, f.db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < 40; i++ {
			id := core.ID(fmt.Sprintf("delivery-%02d", i))
			if err := tx.Put(ctx, deliveriesBucket, persistence.Key("owner", id), Delivery{ID: id, OwnerID: "owner", BindingID: binding.ID, Sequence: uint64(40 - i), CreatedAt: start.Add(time.Duration(i) * time.Second)}); err != nil {
				return err
			}
		}
		return nil
	}))
	status, err := f.s.Status(t.Context(), f.owner, "assistant")
	must(t, err)
	if len(status.Deliveries) != 30 {
		t.Fatal(len(status.Deliveries))
	}
	for i, d := range status.Deliveries {
		if d.Sequence != uint64(i+1) {
			t.Fatalf("creation selection or sequence presentation changed: %+v", d)
		}
	}
}
