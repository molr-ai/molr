package conversation

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func TestOutboxSignalsOnlyCommittedNewMessages(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "outbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	watch, err := db.Watch(OutboxSignal())
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Close()
	e := MessageCreated{ID: "event", OwnerID: "owner", ConversationID: "chat", MessageID: "message", SenderID: "owner", Sender: User, At: time.Now().UTC()}
	rollback := errors.New("rollback")
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := NewOutbox(tx.Store()).Append(ctx, e); err != nil {
			return err
		}
		select {
		case <-watch.Changes():
			t.Fatal("woke before commit")
		default:
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	select {
	case <-watch.Changes():
		t.Fatal("rollback woke consumer")
	default:
	}
	if err := NewOutbox(db).Append(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	select {
	case <-watch.Changes():
	default:
		t.Fatal("committed message did not wake consumer")
	}
	if err := NewOutbox(db).Append(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	select {
	case <-watch.Changes():
		t.Fatal("duplicate woke consumer")
	default:
	}
}
