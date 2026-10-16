package telegram

import (
	"context"
	"errors"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/tests/testkit"
	"strings"
	"testing"
	"time"
)

func TestIdleChannelDoesNotAccessDatabase(t *testing.T) {
	f := setup(t)
	store := &testkit.IdleStore{Store: f.db}
	f.s.store = store
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.s.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			must(t, err)
		case <-time.After(3 * time.Second):
			t.Error("channel failed to join")
		}
	})
	store.RequireIdle(t)
}

func TestActiveChannelSleepsAndWakesCommittedDeliveryAtDeadline(t *testing.T) {
	f := setup(t)
	b := f.bind(t)
	sent := make(chan string, 16)
	f.client.onSend = func() { sent <- f.client.sent[len(f.client.sent)-1] }
	store := &testkit.IdleStore{Store: f.db}
	f.s.store = store
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.s.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			must(t, err)
		case <-time.After(3 * time.Second):
			t.Error("channel failed to join")
		}
	})
	store.RequireIdle(t)
	for len(sent) > 0 {
		<-sent
	}
	publish := func(ctx context.Context) error {
		return f.s.Published(ctx, "owner", conversation.Message{ID: "wake-test", ConversationID: b.ConversationID, Sequence: 99, Content: conversation.Content{Kind: conversation.Text, Text: "wake"}})
	}
	rollback := errors.New("rollback")
	err := f.db.Write(ctx, func(ctx context.Context, _ persistence.Tx) error {
		if err := publish(ctx); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	select {
	case <-sent:
		t.Fatal("rolled back delivery sent")
	case <-time.After(60 * time.Millisecond):
	}
	at := time.Now().Add(200 * time.Millisecond)
	must(t, f.db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := publish(ctx); err != nil {
			return err
		}
		rows, err := persistence.Select[Delivery](ctx, tx.Store(), deliveriesBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("MessageID", "wake-test")}})
		if err != nil {
			return err
		}
		for _, d := range rows {
			d.NextAttempt = at
			if err := f.s.saveDelivery(ctx, tx, d); err != nil {
				return err
			}
		}
		return f.s.Published(ctx, "owner", conversation.Message{ID: "wake-after", ConversationID: b.ConversationID, Sequence: 100, Content: conversation.Content{Kind: conversation.Text, Text: "after"}})
	}))
	select {
	case text := <-sent:
		if !strings.Contains(text, "wake") {
			t.Fatal("later message overtook delayed queue head", text)
		}
		if time.Now().Before(at) {
			t.Fatal("delivery sent before retry deadline")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("committed delivery did not wake")
	}
	select {
	case text := <-sent:
		if !strings.Contains(text, "after") {
			t.Fatal("wrong next message", text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("next queued message did not run")
	}
	store.RequireIdle(t)
}
