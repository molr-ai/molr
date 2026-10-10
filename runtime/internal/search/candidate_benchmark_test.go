package search

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

// Real batched message traversal and SQLite-backed authorization, with sparse
// matches. The policy fixture controls access without a model or HTTP service.
func BenchmarkSearchSparseCandidates(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "search.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			err = db.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
				if err := tx.Put(ctx, "im.conversations", "owner/chat", conversation.Conversation{ID: "chat", OwnerID: "owner", PeerID: "assistant"}); err != nil {
					return err
				}
				for i, doc := range candidateDocuments(count) {
					message := conversation.Message{ID: doc.Object.ID, ConversationID: "chat", Sequence: uint64(i + 1), Content: conversation.Content{Kind: conversation.Text, Text: doc.Text}}
					if err := tx.Put(ctx, "im.messages", persistence.Key("owner", message.ID), message); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				b.Fatal(err)
			}
			service := NewService(db, Options{Authorizer: searchFixtureAuthorizer{store: db}})
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				result, err := service.Search(b.Context(), owner(), Query{Text: "预算", Sources: []Source{Messages}, Page: core.PageRequest{Limit: 200}})
				if err != nil || result.Counts[Messages] != count/500 {
					b.Fatal(result, err)
				}
			}
		})
	}
}
