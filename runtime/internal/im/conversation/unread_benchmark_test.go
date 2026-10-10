package conversation

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func BenchmarkMarkReadUnreadTail(b *testing.B) {
	db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "unread.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	chat := Conversation{ID: "chat", OwnerID: "owner", PeerID: "assistant", Version: 1, LastSequence: 5000, LastMessageID: "message-5000"}
	err = db.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := 1; i <= 5000; i++ {
			id := core.ID(fmt.Sprintf("message-%04d", i))
			sender := Assistant
			if i%3 == 0 {
				sender = User
			}
			if err := tx.Put(ctx, messagesBucket, persistence.Key("owner", id), Message{ID: id, ConversationID: "chat", Sequence: uint64(i), Sender: sender, Content: Content{Kind: Text, Text: strings.Repeat("x", 1024)}}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	repo := NewRepository(db)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		err := db.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, conversationsBucket, "owner/chat", chat)
		})
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if err := repo.MarkRead(b.Context(), "owner", ReadRequest{ConversationID: "chat", ThroughSequence: 100}); err != nil {
			b.Fatal(err)
		}
	}
}
