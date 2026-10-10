package weixin

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

func TestCanceledChannelOperationsDoNotWaitForBusyGate(t *testing.T) {
	for _, name := range []string{"start_login", "poll_login", "cancel_login", "disconnect"} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			login, err := f.s.StartLogin(t.Context(), f.owner, "assistant")
			must(t, err)
			entered := make(chan struct{})
			var once sync.Once
			authorize := f.s.options.Authorize
			f.s.options.Authorize = func(ctx context.Context, p core.Principal, id core.ID, write bool) (Target, error) {
				target, err := authorize(ctx, p, id, write)
				once.Do(func() { close(entered) })
				return target, err
			}
			gate := f.s.gate(f.owner.OwnerID, "assistant")
			must(t, gate.acquire(t.Context()))
			released := false
			release := func() {
				if !released {
					gate.release()
					released = true
				}
			}
			defer release()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				var err error
				switch name {
				case "start_login":
					_, err = f.s.StartLogin(ctx, f.owner, "assistant")
				case "poll_login":
					_, err = f.s.PollLogin(ctx, f.owner, "assistant", PollRequest{ID: login.ID})
				case "cancel_login":
					err = f.s.CancelLogin(ctx, f.owner, "assistant", login.ID)
				case "disconnect":
					err = f.s.Disconnect(ctx, f.owner, "assistant")
				}
				done <- err
			}()
			<-entered
			cancel()
			select {
			case err := <-done:
				if core.CodeOf(err) != core.Canceled {
					t.Fatal("canceled operation returned wrong error", err)
				}
			case <-time.After(200 * time.Millisecond):
				release()
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("operation did not stop after gate release")
				}
				t.Fatal("canceled operation remained blocked by an unrelated gate holder")
			}
			f.s.mu.Lock()
			pending := f.s.logins[login.ID]
			present := pending != nil
			polling := present && pending.Polling
			f.s.mu.Unlock()
			if !present || polling {
				t.Fatal("aborted operation removed login or left polling active")
			}
			release()
			retried, err := f.s.PollLogin(t.Context(), f.owner, "assistant", PollRequest{ID: login.ID})
			must(t, err)
			if retried.Status != "confirmed" {
				t.Fatal("login did not recover after canceled wait", retried.Status)
			}
		})
	}
}

func TestChannelGateCancellationAndIsolation(t *testing.T) {
	s := &Service{}
	g := s.gate("owner", "assistant")
	must(t, g.acquire(t.Context()))
	for _, scope := range [][2]core.ID{{"other-owner", "assistant"}, {"owner", "other-assistant"}} {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		other := s.gate(scope[0], scope[1])
		must(t, other.acquire(ctx))
		other.release()
		cancel()
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := s.gate("owner", "assistant").acquire(ctx); core.CodeOf(err) != core.DeadlineExceeded {
		t.Fatal("busy gate did not observe deadline", err)
	}
	if len(g) != 1 {
		t.Fatal("timed-out waiter released another holder's gate")
	}
	g.release()
	canceled, stop := context.WithCancel(t.Context())
	stop()
	for range 100 {
		if err := g.acquire(canceled); core.CodeOf(err) != core.Canceled {
			t.Fatal("canceled caller acquired free gate", err)
		}
	}
	must(t, g.acquire(t.Context()))
	g.release()
}

func TestChannelGateSerializesConcurrentCallers(t *testing.T) {
	s := &Service{}
	var active, overlaps atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 20 {
				g := s.gate("owner", "assistant")
				if err := g.acquire(t.Context()); err != nil {
					t.Error(err)
					return
				}
				if active.Add(1) != 1 {
					overlaps.Add(1)
				}
				active.Add(-1)
				g.release()
			}
		})
	}
	wg.Wait()
	if overlaps.Load() != 0 {
		t.Fatal("same-assistant operations overlapped", overlaps.Load())
	}
}

func TestCanceledGateWaitPreservesBindingAndPendingDelivery(t *testing.T) {
	f := setup(t)
	b := f.bind(t)
	must(t, f.s.ingest(t.Context(), b, Updates{Messages: []Incoming{incoming("gate-input", "hello")}}))
	f.publish(t, "gate-reply", "answer")
	deliveries, err := persistence.Scan[Delivery](t.Context(), f.db, deliveriesBucket)
	must(t, err)
	if len(deliveries) != 1 || deliveries[0].State != "pending" {
		t.Fatal("expected one pending delivery", deliveries)
	}
	g := f.s.gate(f.owner.OwnerID, "assistant")
	must(t, g.acquire(t.Context()))
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() { f.s.deliver(ctx, deliveries[0]); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		g.release()
		<-done
		t.Fatal("canceled worker remained blocked by gate")
	}
	if err := f.s.Disconnect(ctx, f.owner, "assistant"); core.CodeOf(err) != core.DeadlineExceeded {
		g.release()
		t.Fatal("expired disconnect returned wrong error", err)
	}
	g.release()
	current, err := f.s.binding(t.Context(), "owner", "chat")
	must(t, err)
	if current.ID != b.ID {
		t.Fatal("aborted disconnect changed binding")
	}
	d, err := persistence.Load[Delivery](t.Context(), f.db, deliveriesBucket, persistence.Key("owner", deliveries[0].ID))
	must(t, err)
	if d.State != "pending" || d.Attempts != 0 || len(f.client.sent) != 0 {
		t.Fatal("aborted worker changed or sent pending delivery", d)
	}
	f.s.deliver(t.Context(), d)
	d, err = persistence.Load[Delivery](t.Context(), f.db, deliveriesBucket, persistence.Key("owner", d.ID))
	must(t, err)
	if d.State != "accepted" || d.Attempts != 1 || len(f.client.sent) != 1 {
		t.Fatal("healthy retry did not deliver exactly once", d)
	}
}
