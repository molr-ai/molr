package weixin

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func BenchmarkDeliveryStatus(b *testing.B) {
	db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "status.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	s := &Service{store: db, options: Options{Authorize: func(context.Context, core.Principal, core.ID, bool) (Target, error) {
		return Target{ConversationID: "chat", Active: true}, nil
	}}}
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	err = db.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, bindingsBucket, "owner/chat", Binding{ID: "binding", OwnerID: "owner", ConversationID: "chat", State: "connected", ConnectedAt: at}); err != nil {
			return err
		}
		for i := 0; i < 5000; i++ {
			id := core.ID(fmt.Sprintf("delivery-%05d", i))
			d := Delivery{ID: id, OwnerID: "owner", BindingID: "binding", Sequence: uint64(i + 1), State: "accepted", Text: strings.Repeat("x", 3500), CreatedAt: at.Add(time.Duration(i) * time.Second)}
			if err := tx.Put(ctx, deliveriesBucket, persistence.Key("owner", id), d); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	p := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		status, err := s.Status(b.Context(), p, "assistant")
		if err != nil || len(status.Deliveries) != 30 {
			b.Fatalf("deliveries=%d err=%v", len(status.Deliveries), err)
		}
	}
}
