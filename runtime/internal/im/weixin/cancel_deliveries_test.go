package weixin

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

type cancelDeliveryStore struct {
	persistence.Store
	rows, writes int
	failKey      string
}
type cancelDeliveryTx struct {
	persistence.Tx
	observed *cancelDeliveryStore
}

func (s *cancelDeliveryStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, &cancelDeliveryTx{tx, s}) })
}
func (s *cancelDeliveryStore) Write(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, &cancelDeliveryTx{tx, s}) })
}
func (t *cancelDeliveryTx) Store() persistence.Store { return t.observed }
func (t *cancelDeliveryTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	rows, err := t.Tx.Query(ctx, bucket, q)
	if bucket == deliveriesBucket {
		t.observed.rows += len(rows)
	}
	return rows, err
}
func (t *cancelDeliveryTx) Put(ctx context.Context, bucket, key string, value any) error {
	if bucket == deliveriesBucket {
		if key == t.observed.failKey {
			return core.NewError(core.Unavailable, "injected delivery write failure")
		}
		t.observed.writes++
	}
	return t.Tx.Put(ctx, bucket, key, value)
}

func cancellationFixture(tb testing.TB) (*Service, *cancelDeliveryStore, Binding) {
	tb.Helper()
	db, err := sqlitestore.Open(filepath.Join(tb.TempDir(), "cancel.db"))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { db.Close() })
	observed := &cancelDeliveryStore{Store: db}
	b := Binding{ID: "binding", OwnerID: "owner", AssistantID: "assistant", ConversationID: "chat", State: "login_required"}
	s := &Service{store: observed, options: Options{Authorize: func(_ context.Context, p core.Principal, id core.ID, _ bool) (Target, error) {
		if p.OwnerID != b.OwnerID || id != b.AssistantID {
			return Target{}, core.NewError(core.Forbidden, "denied")
		}
		return Target{ConversationID: b.ConversationID, Active: true}, nil
	}}}
	err = db.Write(tb.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, bindingsBucket, "owner/chat", b); err != nil {
			return err
		}
		terminal := []string{"accepted", "unknown", "failed", "attachment_failed", "canceled"}
		for i := range 1000 {
			d := Delivery{ID: core.ID(fmt.Sprintf("history-%04d", i)), BindingID: b.ID, OwnerID: b.OwnerID, AssistantID: b.AssistantID, ConversationID: b.ConversationID, State: terminal[i%len(terminal)], Text: strings.Repeat("h", 3500), CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
			if err := tx.Put(ctx, deliveriesBucket, persistence.Key(d.OwnerID, d.ID), d); err != nil {
				return err
			}
		}
		for i, state := range []string{"pending", "waiting", "sending"} {
			d := Delivery{ID: core.ID(fmt.Sprintf("active-%d", i)), BindingID: b.ID, OwnerID: b.OwnerID, AssistantID: b.AssistantID, ConversationID: b.ConversationID, State: state, Text: "unsent fixture"}
			if err := tx.Put(ctx, deliveriesBucket, persistence.Key(d.OwnerID, d.ID), d); err != nil {
				return err
			}
		}
		for _, d := range []Delivery{{ID: "unrelated", OwnerID: b.OwnerID, BindingID: "other", State: "pending"}, {ID: "foreign", OwnerID: "foreign", BindingID: b.ID, State: "waiting"}} {
			if err := tx.Put(ctx, deliveriesBucket, persistence.Key(d.OwnerID, d.ID), d); err != nil {
				return err
			}
		}
		if err := s.saveQuote(ctx, tx, b, "reference", quoteRecord{BindingID: b.ID, Text: "quoted fixture"}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	return s, observed, b
}
func TestDisconnectLoadsOnlyUnfinishedDeliveries(t *testing.T) {
	s, db, b := cancellationFixture(t)
	before, err := persistence.Scan[Delivery](t.Context(), db.Store, deliveriesBucket)
	must(t, err)
	must(t, s.Disconnect(t.Context(), core.Principal{Kind: core.ActorOwner, OwnerID: b.OwnerID, SubjectID: b.OwnerID, SessionID: "session"}, b.AssistantID))
	t.Logf("disconnect loaded %d delivery records and wrote %d", db.rows, db.writes)
	if db.rows != 3 || db.writes != 3 {
		t.Fatal("disconnect loaded historical deliveries", db.rows, db.writes)
	}
	after, err := persistence.Scan[Delivery](t.Context(), db.Store, deliveriesBucket)
	must(t, err)
	for i := range before {
		if before[i].OwnerID == b.OwnerID && before[i].BindingID == b.ID && strings.HasPrefix(string(before[i].ID), "active-") {
			before[i].State = "canceled"
		}
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("disconnect changed history, unrelated delivery data or active payloads")
	}
	for i := range 3 {
		d, err := persistence.Load[Delivery](t.Context(), db, deliveriesBucket, fmt.Sprintf("owner/active-%d", i))
		must(t, err)
		if d.State != "canceled" {
			t.Fatal(d)
		}
	}
	for _, key := range []string{"owner/unrelated", "foreign/foreign"} {
		d, err := persistence.Load[Delivery](t.Context(), db, deliveriesBucket, key)
		must(t, err)
		if d.State == "canceled" {
			t.Fatal("scope leaked", key)
		}
	}
	if _, err := s.binding(t.Context(), b.OwnerID, b.ConversationID); !persistence.Missing(err) {
		t.Fatal("binding retained", err)
	}
	if _, err := persistence.Load[quoteRecord](t.Context(), db, quotesBucket, quoteKey(b, "reference")); !persistence.Missing(err) {
		t.Fatal("quote retained", err)
	}
}
func BenchmarkCancelDeliveries(b *testing.B) {
	s, db, binding := cancellationFixture(b)
	rollback := errors.New("benchmark rollback")
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		err := db.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
			if err := s.cancelDeliveries(ctx, tx, binding); err != nil {
				return err
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(db.rows)/float64(b.N), "delivery-records/op")
}

func TestDeliveryCancellationRollsBackWithBindingAndQuotes(t *testing.T) {
	s, db, b := cancellationFixture(t)
	must(t, notification.Sync(t.Context(), db, b.OwnerID, notification.Input{Kind: "weixin.delivery", Code: "waiting", Source: core.Object{Kind: "weixin_delivery", ID: "active-0"}, AssistantID: b.AssistantID}, true))
	db.failKey = "owner/active-1"
	p := core.Principal{Kind: core.ActorOwner, OwnerID: b.OwnerID, SubjectID: b.OwnerID, SessionID: "session"}
	if err := s.Disconnect(t.Context(), p, b.AssistantID); core.CodeOf(err) != core.Unavailable {
		t.Fatal("write failure lost", err)
	}
	for i, state := range []string{"pending", "waiting", "sending"} {
		d, err := persistence.Load[Delivery](t.Context(), db, deliveriesBucket, fmt.Sprintf("owner/active-%d", i))
		must(t, err)
		if d.State != state {
			t.Fatal("partial cancellation persisted", d)
		}
	}
	if _, err := s.binding(t.Context(), b.OwnerID, b.ConversationID); err != nil {
		t.Fatal("binding removed after rollback", err)
	}
	if _, err := persistence.Load[quoteRecord](t.Context(), db, quotesBucket, quoteKey(b, "reference")); err != nil {
		t.Fatal("quote lost after rollback", err)
	}
	unresolved, err := notification.Unresolved(t.Context(), db, "weixin.delivery")
	must(t, err)
	if len(unresolved) != 1 {
		t.Fatal("warning resolved despite rollback", unresolved)
	}
	db.failKey = ""
	must(t, s.Disconnect(t.Context(), p, b.AssistantID))
	unresolved, err = notification.Unresolved(t.Context(), db, "weixin.delivery")
	must(t, err)
	if len(unresolved) != 0 {
		t.Fatal("warning retained after cancellation", unresolved)
	}
}
