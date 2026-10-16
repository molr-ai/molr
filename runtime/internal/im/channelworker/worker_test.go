package channelworker

import (
	"context"
	"errors"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

const bucket = "fixture.bindings"

type fixtureBinding struct {
	Binding
	Enabled bool
}
type job struct {
	At   time.Time
	Done bool
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func await(t *testing.T, c <-chan core.ID) core.ID {
	t.Helper()
	select {
	case id := <-c:
		return id
	case <-time.After(3 * time.Second):
		t.Fatal("missing transition")
		return ""
	}
}
func start(t *testing.T, o Options[fixtureBinding]) {
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, o) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			must(t, err)
		case <-time.After(3 * time.Second):
			t.Error("worker did not join")
		}
	})
}
func newDB(t *testing.T) *sqlitestore.Store {
	t.Helper()
	s, err := sqlitestore.Open(filepath.Join(t.TempDir(), "data.db"))
	must(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}
func put(t *testing.T, s persistence.Store, b fixtureBinding) {
	t.Helper()
	must(t, s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, bucket, persistence.Key(b.OwnerID, b.ConversationID), b); err != nil {
			return err
		}
		return Changed(ctx, tx, bucket, b.OwnerID, b.ConversationID)
	}))
}
func TestRunCommitRollbackDeadlineAndScope(t *testing.T) {
	db := newDB(t)
	store := &testkit.IdleStore{Store: db}
	a := fixtureBinding{Binding: Binding{ID: "a", OwnerID: "owner", AssistantID: "assistant", ConversationID: "a", Work: true}, Enabled: true}
	b := a
	b.ID = "b"
	b.ConversationID = "b"
	put(t, db, a)
	put(t, db, b)
	done := make(chan core.ID, 8)
	var bReads atomic.Int32
	o := Options[fixtureBinding]{Store: store, Bucket: bucket, Describe: func(b fixtureBinding) Binding { return b.Binding }, Live: func(context.Context, core.ID, core.ID) error { return nil }, Step: func(ctx context.Context, b fixtureBinding) (time.Time, error) {
		if b.ID == "b" {
			bReads.Add(1)
		}
		j, err := persistence.Load[job](ctx, db, "jobs", string(b.ID))
		if persistence.Missing(err) {
			return time.Time{}, nil
		}
		if err != nil {
			return time.Time{}, err
		}
		if j.Done {
			return time.Time{}, nil
		}
		if time.Now().Before(j.At) {
			return j.At, nil
		}
		j.Done = true
		err = db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return tx.Put(ctx, "jobs", string(b.ID), j) })
		if err == nil {
			done <- b.ID
		}
		return time.Time{}, err
	}}
	start(t, o)
	store.RequireIdle(t)
	initialB := bReads.Load()
	rollback := errors.New("rollback")
	err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "jobs", "a", job{}); err != nil {
			return err
		}
		if err := Wake(ctx, tx, bucket, "a"); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	select {
	case <-done:
		t.Fatal("rolled back work ran")
	case <-time.After(50 * time.Millisecond):
	}
	at := time.Now().Add(180 * time.Millisecond)
	must(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "jobs", "a", job{At: at}); err != nil {
			return err
		}
		return Wake(ctx, tx, bucket, "a")
	}))
	if id := await(t, done); id != "a" || time.Now().Before(at) {
		t.Fatal("wrong binding or early retry", id)
	}
	if bReads.Load() != initialB {
		t.Fatal("unrelated binding queried")
	}
	store.RequireIdle(t)
}
func TestRunFailedCommitAndDiscoveryAfterStartup(t *testing.T) {
	db := newDB(t)
	store := &testkit.IdleStore{Store: db}
	started := make(chan core.ID, 4)
	o := Options[fixtureBinding]{Store: store, Bucket: bucket, Describe: func(b fixtureBinding) Binding { return b.Binding }, Live: func(context.Context, core.ID, core.ID) error { return nil }, Connect: func(ctx context.Context, b fixtureBinding) { started <- b.ID; <-ctx.Done() }}
	start(t, o)
	store.RequireIdle(t)
	a := fixtureBinding{Binding: Binding{ID: "a", OwnerID: "owner", AssistantID: "assistant", ConversationID: "chat", Connect: true}}
	fail := errors.New("commit failed")
	err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, bucket, "owner/chat", a); err != nil {
			return err
		}
		if err := Changed(ctx, tx, bucket, "owner", "chat"); err != nil {
			return err
		}
		db.FailNextCommit(fail)
		return nil
	})
	if !errors.Is(err, fail) {
		t.Fatal(err)
	}
	select {
	case <-started:
		t.Fatal("failed commit started connection")
	case <-time.After(50 * time.Millisecond):
	}
	put(t, db, a)
	if await(t, started) != "a" {
		t.Fatal("discovery lost")
	}
}
func TestRunRevocationCancelsBusyWorkAndExpiresPairing(t *testing.T) {
	db := newDB(t)
	started, stopped := make(chan core.ID, 8), make(chan core.ID, 8)
	busy := make(chan core.ID, 8)
	var live atomic.Bool
	live.Store(true)
	liveKey := persistence.SignalKey{Namespace: "fixture.live", Key: "owner"}
	a := fixtureBinding{Binding: Binding{ID: "a", OwnerID: "owner", AssistantID: "assistant", ConversationID: "chat", Connect: true, Work: true}}
	put(t, db, a)
	o := Options[fixtureBinding]{Store: db, Bucket: bucket, Describe: func(b fixtureBinding) Binding { return b.Binding }, Live: func(context.Context, core.ID, core.ID) error {
		if !live.Load() {
			return core.NewError(core.Forbidden, "revoked")
		}
		return nil
	}, LiveSignals: func(core.ID, core.ID) []persistence.SignalKey { return []persistence.SignalKey{liveKey} }, Connect: func(ctx context.Context, b fixtureBinding) { started <- b.ID; <-ctx.Done(); stopped <- b.ID }, Step: func(ctx context.Context, b fixtureBinding) (time.Time, error) {
		busy <- b.ID
		<-ctx.Done()
		return time.Time{}, ctx.Err()
	}, Expire: func(ctx context.Context, b fixtureBinding) error {
		return db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return tx.Delete(ctx, bucket, persistence.Key(b.OwnerID, b.ConversationID))
		})
	}}
	start(t, o)
	await(t, started)
	await(t, busy)
	live.Store(false)
	must(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return tx.NotifyAfterCommit(ctx, liveKey) }))
	await(t, stopped)
	live.Store(true)
	must(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return tx.NotifyAfterCommit(ctx, liveKey) }))
	await(t, started)
	await(t, busy)
	a.Expires = time.Now().Add(100 * time.Millisecond)
	put(t, db, a)
	await(t, stopped)
	_, err := persistence.Load[fixtureBinding](t.Context(), db, bucket, "owner/chat")
	if !persistence.Missing(err) {
		t.Fatal("expired binding survived")
	}
}

func TestConnectionKeySerializesRebindingAcrossConversations(t *testing.T) {
	db := newDB(t)
	started := make(chan core.ID, 4)
	a := fixtureBinding{Binding: Binding{ID: "a", OwnerID: "owner", AssistantID: "assistant", ConversationID: "a", Connect: true}}
	b := a
	b.ID = "b"
	b.ConversationID = "b"
	put(t, db, a)
	put(t, db, b)
	start(t, Options[fixtureBinding]{Store: db, Bucket: bucket, Describe: func(b fixtureBinding) Binding { return b.Binding }, Live: func(context.Context, core.ID, core.ID) error { return nil }, ConnectionKey: func(fixtureBinding) string { return "same-bot" }, Connect: func(ctx context.Context, b fixtureBinding) { started <- b.ID; <-ctx.Done() }})
	first := await(t, started)
	select {
	case <-started:
		t.Fatal("overlapping pollers for one bot")
	case <-time.After(70 * time.Millisecond):
	}
	must(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Delete(ctx, bucket, "owner/"+string(first)); err != nil {
			return err
		}
		return Changed(ctx, tx, bucket, "owner", first)
	}))
	if second := await(t, started); second == first {
		t.Fatal("old generation restarted")
	}
}
