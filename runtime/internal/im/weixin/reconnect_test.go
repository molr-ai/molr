package weixin

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/persistence"
)

type pollReply struct {
	updates Updates
	err     error
}
type pollCall struct {
	ctx    context.Context
	cursor string
	reply  chan pollReply
}
type reconnectClient struct {
	*testClient
	calls chan pollCall
}

func (c *reconnectClient) Poll(ctx context.Context, _, _, cursor string) (Updates, error) {
	call := pollCall{ctx: ctx, cursor: cursor, reply: make(chan pollReply, 1)}
	select {
	case c.calls <- call:
	case <-ctx.Done():
		return Updates{}, ctx.Err()
	}
	select {
	case r := <-call.reply:
		return r.updates, r.err
	case <-ctx.Done():
		return Updates{}, ctx.Err()
	}
}
func nextPoll(t *testing.T, calls <-chan pollCall) pollCall {
	t.Helper()
	select {
	case c := <-calls:
		return c
	case <-time.After(10 * time.Second):
		t.Fatal("poll worker did not resume")
		return pollCall{}
	}
}
func awaitConnected(t *testing.T, f *fixture) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b, err := f.s.binding(t.Context(), "owner", "chat")
		must(t, err)
		if b.State == "connected" && b.LastError == "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("successful poll did not restore connection")
}
func TestPollingSurvivesFailureAndRestartsReconnectingBinding(t *testing.T) {
	for _, restart := range []bool{false, true} {
		name := "live retry"
		if restart {
			name = "persisted reconnect"
		}
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			b := f.bind(t)
			must(t, f.s.ingest(t.Context(), b, Updates{Cursor: "saved-cursor"}))
			if restart {
				f.s.setFailure(t.Context(), b, "unknown")
			}
			client := &reconnectClient{testClient: f.client, calls: make(chan pollCall)}
			f.s.options.Client = client
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- f.s.Run(ctx) }()
			defer func() {
				cancel()
				select {
				case err := <-done:
					must(t, err)
				case <-time.After(3 * time.Second):
					t.Error("workers did not stop")
				}
			}()
			call := nextPoll(t, client.calls)
			if call.cursor != "saved-cursor" {
				t.Fatal("lost persisted cursor")
			}
			if !restart {
				call.reply <- pollReply{err: errors.New("temporary transport failure")}
				// Span multiple supervisor ticks and the real five-second retry delay.
				call = nextPoll(t, client.calls)
				if call.cursor != "saved-cursor" {
					t.Fatal("retry lost cursor")
				}
			}
			call.reply <- pollReply{updates: Updates{Cursor: "recovered-cursor"}}
			awaitConnected(t, f)
			next := nextPoll(t, client.calls)
			if next.cursor != "recovered-cursor" {
				t.Fatal("recovered cursor not used")
			}
			must(t, f.s.Disconnect(t.Context(), f.owner, "assistant"))
			select {
			case <-next.ctx.Done():
			case <-time.After(3 * time.Second):
				t.Fatal("unbind left poll worker running")
			}
		})
	}
}

func TestConnectionNotificationsDeduplicateDismissRecoverAndEscalate(t *testing.T) {
	f := setup(t)
	b := f.bind(t)
	ctx := t.Context()
	notices := notification.New(f.db, testAuthorizer{})
	active := func() []notification.Notice {
		t.Helper()
		page, err := notices.List(ctx, f.owner, notification.Query{Filter: "active", Kind: "weixin.connection"})
		must(t, err)
		return page.Items
	}
	// Persisted reconnects must gain a notice even if the error hasn't changed.
	b.State = "reconnecting"
	b.LastError = "unknown"
	must(t, f.db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, bindingsBucket, persistence.Key(b.OwnerID, b.ConversationID), b)
	}))
	f.s.setFailure(ctx, b, "unknown")
	rows := active()
	if len(rows) != 1 || rows[0].Code != "reconnecting" {
		t.Fatal(rows)
	}
	first := rows[0].ID
	f.s.setFailure(ctx, b, "unknown")
	f.s.setFailure(ctx, b, "rate_limited")
	rows = active()
	if len(rows) != 1 || rows[0].ID != first {
		t.Fatal("retries created duplicate notices", rows)
	}
	must(t, notices.Update(ctx, f.owner, []core.ID{first}, "dismiss"))
	f.s.setFailure(ctx, b, "unknown")
	if len(active()) != 0 {
		t.Fatal("retry rearmed dismissed notice")
	}
	must(t, f.s.ingest(ctx, b, Updates{Cursor: "ok"}))
	stored, err := persistence.Load[notification.Notice](ctx, f.db, "notifications.records", persistence.Key(f.owner.OwnerID, first))
	must(t, err)
	if stored.ResolvedAt == nil {
		t.Fatal("recovery did not resolve dismissed notice")
	}
	f.s.setFailure(ctx, b, "unknown")
	rows = active()
	if len(rows) != 1 || rows[0].ID == first {
		t.Fatal("new outage did not rearm", rows)
	}
	reconnectID := rows[0].ID
	f.s.setFailure(ctx, b, "login_required")
	rows = active()
	if len(rows) != 1 || rows[0].Code != "login_required" || rows[0].ID == reconnectID {
		t.Fatal("login failure did not replace reconnect notice", rows)
	}
	must(t, f.s.Disconnect(ctx, f.owner, "assistant"))
	if len(active()) != 0 {
		t.Fatal("unbind did not resolve notice")
	}
}

func TestPollBackoffDoublesCapsResetsAndCancels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := setup(t)
		b := f.bind(t)
		client := &reconnectClient{testClient: f.client, calls: make(chan pollCall)}
		f.s.options.Client = client
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan struct{})
		go func() { defer close(done); f.s.pollLoop(ctx, b) }()
		next := func() pollCall {
			t.Helper()
			select {
			case call := <-client.calls:
				return call
			case <-time.After(20 * time.Minute):
				t.Fatal("poll did not retry within the maximum delay")
				return pollCall{}
			}
		}
		call := next()
		for _, delay := range []time.Duration{5, 10, 20, 40, 80, 160, 320, 640, 640} {
			started := time.Now()
			call.reply <- pollReply{err: errors.New("temporary failure")}
			call = next()
			if elapsed := time.Since(started); elapsed != delay*time.Second {
				t.Fatalf("retry waited %v, want %vs", elapsed, delay)
			}
		}
		// A successfully committed poll resets the next failure to the base delay.
		call.reply <- pollReply{updates: Updates{Cursor: "recovered"}}
		call = next()
		started := time.Now()
		call.reply <- pollReply{err: errors.New("new outage")}
		call = next()
		if elapsed := time.Since(started); elapsed != 5*time.Second {
			t.Fatalf("recovery did not reset delay: %v", elapsed)
		}
		// Cancellation interrupts the timer instead of waiting for another retry.
		call.reply <- pollReply{err: errors.New("still unavailable")}
		synctest.Wait()
		started = time.Now()
		cancel()
		<-done
		if time.Since(started) != 0 {
			t.Fatal("cancel waited for the backoff timer")
		}
	})
}
