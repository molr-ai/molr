package search

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

// Vary unrelated history without changing the requested source or result set.
func BenchmarkSearchAssistantHistory(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "search.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			err = db.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
				if err := tx.Put(ctx, "business.assistants", "owner/assistant", assistant.Assistant{ID: "assistant", OwnerID: "owner", Name: "Budget assistant", State: assistant.Active}); err != nil {
					return err
				}
				if err := tx.Put(ctx, "im.conversations", "owner/chat", conversation.Conversation{ID: "chat", OwnerID: "owner", PeerID: "assistant"}); err != nil {
					return err
				}
				for i := 0; i < count; i++ {
					id := core.ID(fmt.Sprintf("m-%06d", i))
					if err := tx.Put(ctx, "im.messages", persistence.Key("owner", id), conversation.Message{ID: id, ConversationID: "chat", Sequence: uint64(i + 1), Content: conversation.Content{Kind: conversation.Text, Text: "Unrelated historical message"}}); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				b.Fatal(err)
			}
			service := NewService(db, Options{Authorizer: fixtureAuthorizer{}})
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				result, err := service.Search(b.Context(), owner(), Query{Text: "budget", Sources: []Source{Assistants}})
				if err != nil || len(result.Hits.Items) != 1 {
					b.Fatal(result, err)
				}
			}
		})
	}
}
