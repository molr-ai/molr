package conversation

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"path/filepath"
	"testing"
	"time"
)

type feedbackReadStore struct {
	persistence.Store
	reads int
}
type feedbackReadTx struct {
	persistence.Tx
	parent *feedbackReadStore
}

func (s *feedbackReadStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, &feedbackReadTx{Tx: tx, parent: s}) })
}
func (t *feedbackReadTx) Get(ctx context.Context, bucket, key string, out any) error {
	if bucket == feedbackBucket {
		t.parent.reads++
	}
	return t.Tx.Get(ctx, bucket, key, out)
}
func (t *feedbackReadTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	if bucket == feedbackBucket {
		t.parent.reads++
	}
	return t.Tx.Query(ctx, bucket, q)
}
func feedbackBatchFixture(tb testing.TB) (Repository, *feedbackReadStore) {
	tb.Helper()
	db, err := sqlitestore.Open(filepath.Join(tb.TempDir(), "feedback.db"))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { db.Close() })
	err = db.Write(tb.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, conversationsBucket, "owner/chat", Conversation{ID: "chat", OwnerID: "owner", PeerID: "assistant", LastSequence: 200, Version: 1}); err != nil {
			return err
		}
		for i := range 200 {
			id := core.ID(fmt.Sprintf("message-%03d", i))
			m := Message{ID: id, ConversationID: "chat", Sequence: uint64(i + 1), Sender: Assistant, SenderID: "assistant", Content: Content{Kind: Text, Text: "Fixture reply"}, CreatedAt: time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC)}
			if err := tx.Put(ctx, messagesBucket, persistence.Key("owner", id), m); err != nil {
				return err
			}
			if i%2 == 0 {
				if err := tx.Put(ctx, feedbackBucket, persistence.Key("owner", id), Like); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	observed := &feedbackReadStore{Store: db}
	return NewRepository(observed), observed
}
func TestMessagePageBatchesFeedbackReads(t *testing.T) {
	repo, observed := feedbackBatchFixture(t)
	page, err := repo.Messages(t.Context(), "owner", MessageQuery{ConversationID: "chat", Page: core.PageRequest{Limit: 50}})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 50 || page.NextCursor == "" {
		t.Fatal("page changed", len(page.Items))
	}
	for i, m := range page.Items {
		want := NoFeedback
		if i%2 == 0 {
			want = Like
		}
		if m.Feedback != want {
			t.Fatalf("message %s feedback=%s want=%s", m.ID, m.Feedback, want)
		}
	}
	if observed.reads > 1 {
		t.Fatalf("50-message page made %d separate feedback reads", observed.reads)
	}
}
func BenchmarkMessagePageFeedback(b *testing.B) {
	repo, observed := feedbackBatchFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		page, err := repo.Messages(b.Context(), "owner", MessageQuery{ConversationID: "chat", Page: core.PageRequest{Limit: 50}})
		if err != nil || len(page.Items) != 50 {
			b.Fatalf("messages=%d err=%v", len(page.Items), err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(observed.reads)/float64(b.N), "feedback-reads/op")
}

func TestFeedbackBatchRespectsOwnerPageAndMessageFilter(t *testing.T) {
	repo, observed := feedbackBatchFixture(t)
	err := observed.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := range 200 {
			id := core.ID(fmt.Sprintf("message-%03d", i))
			if err := tx.Put(ctx, feedbackBucket, persistence.Key("other", id), Dislike); err != nil {
				return err
			}
		}
		// Bad data beyond the first page must not break that page's feedback read.
		return tx.Put(ctx, feedbackBucket, "owner/message-050", map[string]bool{"invalid": true})
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := repo.Messages(t.Context(), "owner", MessageQuery{ConversationID: "chat", Page: core.PageRequest{Limit: 50}})
	if err != nil {
		t.Fatal(err)
	}
	if first.Items[0].Feedback != Like || first.Items[1].Feedback != NoFeedback {
		t.Fatal("feedback crossed owner boundary")
	}
	for _, q := range []MessageQuery{
		{ConversationID: "chat", MessageID: "message-003"},
		{ConversationID: "chat", Descending: true, Page: core.PageRequest{Limit: 50}},
		{ConversationID: "chat", AfterSequence: 195, Page: core.PageRequest{Limit: 50}},
	} {
		observed.reads = 0
		page, err := repo.Messages(t.Context(), "owner", q)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range page.Items {
			want := NoFeedback
			if (m.Sequence-1)%2 == 0 {
				want = Like
			}
			if m.Feedback != want {
				t.Fatalf("sequence %d feedback=%s want=%s", m.Sequence, m.Feedback, want)
			}
		}
		if observed.reads != 1 {
			t.Fatal("feedback query count", observed.reads)
		}
	}
	observed.reads = 0
	empty, err := repo.Messages(t.Context(), "owner", MessageQuery{ConversationID: "chat", AfterSequence: 200})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Items) != 0 || observed.reads != 0 {
		t.Fatal("empty page read unrelated feedback")
	}
	if _, err := repo.Messages(t.Context(), "other", MessageQuery{ConversationID: "chat"}); core.CodeOf(err) != core.NotFound {
		t.Fatal("foreign conversation accepted", err)
	}
	if _, err := repo.Messages(t.Context(), "owner", MessageQuery{ConversationID: "chat", Page: core.PageRequest{Limit: 50, Cursor: first.NextCursor}}); core.CodeOf(err) != core.Internal {
		t.Fatal("invalid selected feedback ignored", err)
	}
}
func TestFeedbackBatchReadsFreshUpdatesAcrossPages(t *testing.T) {
	repo, observed := feedbackBatchFixture(t)
	first, err := repo.Messages(t.Context(), "owner", MessageQuery{ConversationID: "chat", Page: core.PageRequest{Limit: 50}})
	if err != nil {
		t.Fatal(err)
	}
	err = observed.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, feedbackBucket, "owner/message-050", Dislike)
	})
	if err != nil {
		t.Fatal(err)
	}
	next, err := repo.Messages(t.Context(), "owner", MessageQuery{ConversationID: "chat", Page: core.PageRequest{Limit: 50, Cursor: first.NextCursor}})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Items) != 50 || next.Items[0].ID != "message-050" || next.Items[0].Feedback != Dislike {
		t.Fatal("next page lost live feedback")
	}
	err = observed.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Delete(ctx, feedbackBucket, "owner/message-050")
	})
	if err != nil {
		t.Fatal(err)
	}
	found, err := repo.Messages(t.Context(), "owner", MessageQuery{ConversationID: "chat", MessageID: "message-050"})
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Items) != 1 || found.Items[0].Feedback != NoFeedback {
		t.Fatal("cleared feedback remained cached")
	}
}
