package conversation

import (
	"context"
	"molr.ai/runtime/internal/persistence"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
)

func TestTDDConversationConcurrentRetriesPersistOneSendAcrossRestart(t *testing.T) {
	ctx := testkit.Context(t)
	path := filepath.Join(t.TempDir(), "durable.db")
	db, err := sqlitestore.Open(path)
	testkit.RequireNoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	owner := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
	repo := NewRepository(db)
	testkit.RequireNoError(t, repo.Insert(ctx, Conversation{ID: "chat", OwnerID: owner.OwnerID, PeerID: "assistant", Version: 1}))
	service := NewService(db, Options{Authorizer: fixtureAuthorizer{}})
	type result struct {
		message Message
		err     error
	}
	results := make(chan result, 12)
	start := make(chan struct{})
	for range 12 {
		go func() {
			<-start
			message, err := service.SendUser(ctx, owner, sendRequest("chat", "same", "hello"))
			results <- result{message, err}
		}()
	}
	close(start)
	var first core.ID
	for range 12 {
		select {
		case r := <-results:
			testkit.RequireNoError(t, r.err)
			if first == "" {
				first = r.message.ID
			}
			if first != r.message.ID {
				t.Fatal("concurrent retry produced a different message")
			}
		case <-ctx.Done():
			t.Fatal("concurrent send exceeded deadline")
		}
	}
	pending, err := NewOutbox(db).Pending(ctx, 20)
	testkit.RequireNoError(t, err)
	if len(pending) != 1 {
		t.Fatalf("outbox count = %d", len(pending))
	}
	visible, err := realtime.NewStore(db).Replay(ctx, owner.OwnerID, realtime.Query{})
	testkit.RequireNoError(t, err)
	if len(visible.Items) != 1 || visible.Items[0].ConversationID != "chat" {
		t.Fatalf("visible count = %d", len(visible.Items))
	}
	testkit.RequireNoError(t, db.Close())
	db, err = sqlitestore.Open(path)
	testkit.RequireNoError(t, err)
	recovered := NewService(db, Options{Authorizer: fixtureAuthorizer{}})
	message, err := recovered.SendUser(ctx, owner, sendRequest("chat", "same", "hello"))
	testkit.RequireNoError(t, err)
	if message.ID != first {
		t.Fatal("restart lost idempotency receipt")
	}
	page, err := recovered.Messages(ctx, owner, MessageQuery{ConversationID: "chat"})
	testkit.RequireNoError(t, err)
	if len(page.Items) != 1 {
		t.Fatal("restart duplicated message")
	}
}
func TestTDDConversationDefaultAuthorizationFailsClosed(t *testing.T) {
	ctx := testkit.Context(t)
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "auth.db"))
	testkit.RequireNoError(t, err)
	defer db.Close()
	owner := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
	service := NewService(db, Options{})
	_, err = service.SendUser(ctx, owner, sendRequest("chat", "denied", "no"))
	testkit.RequireCode(t, err, core.Unauthenticated)
	_, err = service.List(ctx, owner, core.PageRequest{})
	testkit.RequireCode(t, err, core.Unauthenticated)
}
func TestTDDConversationAssistantCannotPublishOrReadOtherPeer(t *testing.T) {
	f := testkit.RequireFactory(t, "im/conversation", newContractFixture)
	ctx := testkit.Context(t)
	foreign := f.Execution
	foreign.AssistantID = "second-assistant"
	foreign.Principal.SubjectID = foreign.AssistantID
	_, err := f.Service.Publish(ctx, foreign, PublishRequest{Mutation: core.Mutation{IdempotencyKey: "wrong-peer"}, ConversationID: f.Chat.ID, Content: Content{Kind: Text, Text: "bad"}})
	testkit.RequireCode(t, err, core.Forbidden)
	_, err = f.Service.Messages(ctx, foreign.Principal, MessageQuery{ConversationID: f.Chat.ID})
	testkit.RequireCode(t, err, core.Forbidden)
	pending, err := f.Outbox.Pending(context.Background(), 10)
	testkit.RequireNoError(t, err)
	if len(pending) != 0 {
		t.Fatal("wrong-peer publication emitted event")
	}
}

func TestTDDConversationSequenceSurvivesHistoryDeletion(t *testing.T) {
	ctx := testkit.Context(t)
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sequence.db"))
	testkit.RequireNoError(t, err)
	defer db.Close()
	repo := NewRepository(db)
	testkit.RequireNoError(t, repo.Insert(ctx, Conversation{ID: "chat", OwnerID: "owner", PeerID: "assistant", Version: 1}))
	appendMessage := func(id core.ID) Message {
		m, err := repo.Append(ctx, "owner", Message{ID: id, ConversationID: "chat", Sender: User, SenderID: "owner", Content: Content{Kind: Text, Text: "hello"}, CreatedAt: time.Now()})
		testkit.RequireNoError(t, err)
		return m
	}
	first := appendMessage("first")
	second := appendMessage("second")
	testkit.RequireNoError(t, db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Delete(ctx, messagesBucket, persistence.Key("owner", second.ID)); err != nil {
			return err
		}
		chat, err := repo.Get(ctx, "owner", "chat")
		if err != nil {
			return err
		}
		chat.LastMessageID = first.ID
		return tx.Put(ctx, conversationsBucket, persistence.Key("owner", "chat"), chat)
	}))
	third := appendMessage("third")
	if third.Sequence <= second.Sequence {
		t.Fatal("deleted tail sequence was reused")
	}
	testkit.RequireNoError(t, db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		for _, id := range []core.ID{first.ID, third.ID} {
			if err := tx.Delete(ctx, messagesBucket, persistence.Key("owner", id)); err != nil {
				return err
			}
		}
		chat, err := repo.Get(ctx, "owner", "chat")
		if err != nil {
			return err
		}
		chat.LastMessageID = ""
		chat.UnreadCount = 0
		return tx.Put(ctx, conversationsBucket, persistence.Key("owner", "chat"), chat)
	}))
	fourth := appendMessage("fourth")
	if fourth.Sequence <= third.Sequence {
		t.Fatal("cleared conversation sequence restarted")
	}
}
