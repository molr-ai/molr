package debugtrace

import (
	"context"
	"errors"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
)

func TestRevisionWatchCommitRollbackScopeAndClose(t *testing.T) {
	s, p, _ := fixture(t)
	first, err := s.WatchRevision(t.Context(), p)
	testkit.RequireNoError(t, err)
	defer first.Close()
	second, err := s.WatchRevision(t.Context(), p)
	testkit.RequireNoError(t, err)
	defer second.Close()
	other := p
	other.OwnerID, other.SubjectID = "other", "other"
	foreign, err := s.WatchRevision(t.Context(), other)
	testkit.RequireNoError(t, err)
	defer foreign.Close()
	noSignal := func(w persistence.Watch) {
		t.Helper()
		select {
		case <-w.Changes():
			t.Fatal("unexpected notification")
		default:
		}
	}
	rollback := errors.New("rollback")
	bump := func(ctx context.Context, tx persistence.Tx) error { return s.bump(ctx, tx, p.OwnerID) }
	err = s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		testkit.RequireNoError(t, bump(ctx, tx))
		noSignal(first)
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	noSignal(first)
	s.store.(*sqlitestore.Store).FailNextCommit(rollback)
	if err = s.store.Write(t.Context(), bump); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	noSignal(first)
	testkit.RequireNoError(t, s.store.Write(t.Context(), bump))
	for _, w := range []persistence.Watch{first, second} {
		select {
		case <-w.Changes():
		case <-time.After(time.Second):
			t.Fatal("commit failed to notify every reader")
		}
	}
	revision, err := s.Revision(t.Context(), p)
	testkit.RequireNoError(t, err)
	if revision != 1 {
		t.Fatalf("rollback changed revision: %d", revision)
	}
	noSignal(foreign)
	first.Close()
	first.Close()
	select {
	case <-first.Done():
	default:
		t.Fatal("closed watcher still active")
	}
	testkit.RequireNoError(t, s.store.(*sqlitestore.Store).Close())
	select {
	case <-second.Done():
	default:
		t.Fatal("database close left watcher active")
	}
}

func TestRevisionWatchAuthorization(t *testing.T) {
	s, p, _ := fixture(t)
	s.options.Enabled = false
	_, err := s.WatchRevision(t.Context(), p)
	testkit.RequireCode(t, err, core.Forbidden)
	s.options.Enabled = true
	p.Kind, p.SubjectID, p.SessionID, p.ExecutionID = core.ActorAssistant, "assistant", "", "execution"
	_, err = s.WatchRevision(t.Context(), p)
	testkit.RequireCode(t, err, core.Forbidden)
}
