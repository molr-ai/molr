package conversation

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

type unreadCountingStore struct {
	persistence.Store
	messageRows int
}
type unreadCountingTx struct {
	persistence.Tx
	counts *unreadCountingStore
}

func (s *unreadCountingStore) Write(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, &unreadCountingTx{tx, s}) })
}
func (tx *unreadCountingTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	rows, err := tx.Tx.Query(ctx, bucket, q)
	if bucket == messagesBucket {
		tx.counts.messageRows += len(rows)
	}
	return rows, err
}
func TestMarkReadCountsUnreadWithoutLoadingMessageRows(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "unread.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expected := uint64(0)
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, conversationsBucket, "owner/chat", Conversation{ID: "chat", OwnerID: "owner", PeerID: "assistant", Version: 1, LastSequence: 1000}); err != nil {
			return err
		}
		for i := 1; i <= 1000; i++ {
			sender := Assistant
			if i%3 == 0 {
				sender = User
			}
			if i > 10 && sender == Assistant {
				expected++
			}
			id := core.ID(fmt.Sprintf("message-%04d", i))
			m := Message{ID: id, ConversationID: "chat", Sequence: uint64(i), Sender: sender, Content: Content{Kind: Text, Text: "synthetic unread-count fixture"}}
			if err := tx.Put(ctx, messagesBucket, persistence.Key("owner", id), m); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	counts := &unreadCountingStore{Store: db}
	repo := NewRepository(counts)
	if err := repo.MarkRead(t.Context(), "owner", ReadRequest{ConversationID: "chat", ThroughSequence: 10}); err != nil {
		t.Fatal(err)
	}
	chat, err := repo.Get(t.Context(), "owner", "chat")
	if err != nil {
		t.Fatal(err)
	}
	if chat.LastReadSequence != 10 || chat.UnreadCount != expected {
		t.Fatalf("wrong read state: %+v", chat)
	}
	t.Logf("unread=%d loaded message rows=%d", chat.UnreadCount, counts.messageRows)
	if counts.messageRows != 0 {
		t.Fatalf("loaded %d message rows just to count unread messages", counts.messageRows)
	}
}

func TestMarkReadAggregateScopeMonotonicityAndRollback(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "scope.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewRepository(db)
	for _, chat := range []Conversation{{ID: "chat", OwnerID: "owner", PeerID: "assistant", Version: 1}, {ID: "other", OwnerID: "owner", PeerID: "second", Version: 1}, {ID: "chat", OwnerID: "owner-extra", PeerID: "foreign", Version: 1}} {
		if err := repo.Insert(t.Context(), chat); err != nil {
			t.Fatal(err)
		}
		for i := 1; i <= 30; i++ {
			sender, senderID := Assistant, chat.PeerID
			if chat.OwnerID == "owner" && chat.ID == "chat" && i%3 == 0 {
				sender, senderID = User, chat.OwnerID
			}
			_, err := repo.Append(t.Context(), chat.OwnerID, Message{ID: core.ID(fmt.Sprintf("%s-%02d", chat.ID, i)), ConversationID: chat.ID, Sender: sender, SenderID: senderID, CreatedAt: time.Now(), Content: Content{Kind: Text, Text: "synthetic message"}})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	check := func(read, unread uint64) {
		t.Helper()
		chat, err := repo.Get(t.Context(), "owner", "chat")
		if err != nil || chat.LastReadSequence != read || chat.UnreadCount != unread {
			t.Fatalf("read=%d unread=%d: %+v %v", read, unread, chat, err)
		}
	}
	if err := repo.MarkRead(t.Context(), "owner", ReadRequest{ConversationID: "chat", ThroughSequence: 10}); err != nil {
		t.Fatal(err)
	}
	check(10, 13)
	for _, seq := range []uint64{0, 5, 10} {
		if err := repo.MarkRead(t.Context(), "owner", ReadRequest{ConversationID: "chat", ThroughSequence: seq}); err != nil {
			t.Fatal(err)
		}
		check(10, 13)
	}
	if err := repo.MarkRead(t.Context(), "owner", ReadRequest{ConversationID: "chat", ThroughSequence: 31}); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("future sequence accepted", err)
	}
	check(10, 13)
	db.FailNextCommit(errors.New("synthetic commit failure"))
	if err := repo.MarkRead(t.Context(), "owner", ReadRequest{ConversationID: "chat", ThroughSequence: 20}); err == nil {
		t.Fatal("commit failure hidden")
	}
	check(10, 13)
	for _, scope := range []struct{ owner, chat core.ID }{{"owner", "other"}, {"owner-extra", "chat"}} {
		chat, err := repo.Get(t.Context(), scope.owner, scope.chat)
		if err != nil || chat.LastReadSequence != 0 || chat.UnreadCount != 30 {
			t.Fatal("unrelated conversation changed", chat, err)
		}
	}
	if err := repo.MarkRead(t.Context(), "owner", ReadRequest{ConversationID: "chat", ThroughSequence: 30}); err != nil {
		t.Fatal(err)
	}
	check(30, 0)
}

func TestMarkReadAggregateIsAtomicWithConcurrentAppends(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "concurrent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewRepository(db)
	if err := repo.Insert(t.Context(), Conversation{ID: "chat", OwnerID: "owner", PeerID: "assistant", Version: 1}); err != nil {
		t.Fatal(err)
	}
	appendMessage := func(id core.ID) error {
		_, err := repo.Append(t.Context(), "owner", Message{ID: id, ConversationID: "chat", Sender: Assistant, SenderID: "assistant", Content: Content{Kind: Text, Text: "concurrent fixture"}, CreatedAt: time.Now()})
		return err
	}
	for i := 0; i < 30; i++ {
		if err := appendMessage(core.ID(fmt.Sprintf("initial-%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, 11)
	for i := 0; i < 10; i++ {
		wg.Go(func() { <-start; errs <- appendMessage(core.ID(fmt.Sprintf("later-%d", i))) })
	}
	wg.Go(func() {
		<-start
		errs <- repo.MarkRead(t.Context(), "owner", ReadRequest{ConversationID: "chat", ThroughSequence: 30})
	})
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	chat, err := repo.Get(t.Context(), "owner", "chat")
	if err != nil || chat.LastSequence != 40 || chat.LastReadSequence != 30 || chat.UnreadCount != 10 {
		t.Fatal("lost concurrent unread update", chat, err)
	}
	if err := repo.MarkRead(t.Context(), "owner", ReadRequest{ConversationID: "chat", ThroughSequence: 40}); err != nil {
		t.Fatal(err)
	}
	chat, err = repo.Get(t.Context(), "owner", "chat")
	if err != nil || chat.UnreadCount != 0 {
		t.Fatal("all-read state", chat, err)
	}
}
