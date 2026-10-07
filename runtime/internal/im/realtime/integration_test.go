package realtime

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
)

func TestTDDRealtimeStoreUsesCommittedSequenceAcrossInstances(t *testing.T) {
	ctx := testkit.Context(t)
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "realtime.db"))
	testkit.RequireNoError(t, err)
	defer db.Close()
	a, b := NewStore(db), NewStore(db)
	sentinel := errors.New("abort")
	err = db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := a.Append(ctx, visible("owner", "rolled-back")); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("rollback error = %v", err)
	}
	testkit.RequireNoError(t, b.Append(ctx, visible("owner", "committed")))
	page, err := a.Replay(ctx, "owner", Query{})
	testkit.RequireNoError(t, err)
	if len(page.Items) != 1 || page.Items[0].ID != "committed" || page.Items[0].Sequence != 1 {
		t.Fatalf("uncommitted event/counter visible: %+v", page)
	}
}
func TestTDDRealtimeRevocationInterruptsAlreadyWaitingNext(t *testing.T) {
	f := testkit.RequireFactory(t, "im/realtime", newContractFixture)
	ctx := testkit.Context(t)
	subscription, err := f.Feed.Subscribe(ctx, f.Owner, Query{})
	testkit.RequireNoError(t, err)
	defer subscription.Close()
	result := make(chan error, 1)
	go func() { _, err := subscription.Next(ctx); result <- err }()
	f.Revoke(f.Owner)
	select {
	case err := <-result:
		testkit.RequireCode(t, err, core.Unauthenticated)
	case <-time.After(time.Second):
		t.Fatal("idle subscription did not observe revocation")
	}
}
func TestTDDRealtimeCloseInterruptsAlreadyWaitingNext(t *testing.T) {
	f := testkit.RequireFactory(t, "im/realtime", newContractFixture)
	ctx := testkit.Context(t)
	subscription, err := f.Feed.Subscribe(ctx, f.Owner, Query{})
	testkit.RequireNoError(t, err)
	result := make(chan error, 1)
	go func() { _, err := subscription.Next(ctx); result <- err }()
	testkit.RequireNoError(t, subscription.Close())
	select {
	case err := <-result:
		testkit.RequireCode(t, err, core.Canceled)
	case <-time.After(time.Second):
		t.Fatal("Close did not interrupt idle subscription")
	}
}
func TestTDDRealtimeFeedDefaultsToDeny(t *testing.T) {
	ctx := testkit.Context(t)
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "auth.db"))
	testkit.RequireNoError(t, err)
	defer db.Close()
	feed := NewFeed(NewStore(db), Options{})
	owner := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
	_, err = feed.List(ctx, owner, Query{})
	testkit.RequireCode(t, err, core.Unauthenticated)
	_, err = feed.Subscribe(ctx, owner, Query{})
	testkit.RequireCode(t, err, core.Unauthenticated)
}

func TestTDDRealtimeCursorBindsOwnerAndTopics(t *testing.T) {
	encoded, err := Cursor("owner", []string{"task.updated", "message.created"}, 42)
	testkit.RequireNoError(t, err)
	decoded, err := parseCursor(encoded, "owner", []string{"message.created", "task.updated"})
	testkit.RequireNoError(t, err)
	if decoded != 42 {
		t.Fatal("cursor lost sequence")
	}
	_, err = parseCursor(encoded, "other", []string{"message.created", "task.updated"})
	testkit.RequireCode(t, err, core.InvalidArgument)
	_, err = parseCursor(encoded, "owner", []string{"message.created"})
	testkit.RequireCode(t, err, core.InvalidArgument)
	_, err = Cursor("../owner", nil, 1)
	testkit.RequireCode(t, err, core.InvalidArgument)
	_, err = Cursor("owner", []string{""}, 1)
	testkit.RequireCode(t, err, core.InvalidArgument)
}
