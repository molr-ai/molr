package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/storage/files"
)

type attachmentAccessStore struct {
	persistence.Store
	messageBytes, conversationReads int
}
type attachmentAccessTx struct {
	persistence.Tx
	parent *attachmentAccessStore
}

func (s *attachmentAccessStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, &attachmentAccessTx{tx, s}) })
}
func (t *attachmentAccessTx) Store() persistence.Store { return t.parent }
func (t *attachmentAccessTx) Get(ctx context.Context, bucket, key string, out any) error {
	if bucket == "im.conversations" {
		t.parent.conversationReads++
	}
	return t.Tx.Get(ctx, bucket, key, out)
}
func (t *attachmentAccessTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	rows, err := t.Tx.Query(ctx, bucket, q)
	if bucket == "im.messages" {
		for _, r := range rows {
			t.parent.messageBytes += len(r.Data)
		}
	}
	return rows, err
}
func attachmentAccessFixture(tb testing.TB) (access, *attachmentAccessStore, core.Principal) {
	tb.Helper()
	db, err := sqlitestore.Open(filepath.Join(tb.TempDir(), "access.db"))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { db.Close() })
	err = db.Write(tb.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, files.Bucket, "owner/file", files.File{ID: "file", OwnerID: "owner", State: files.Ready}); err != nil {
			return err
		}
		if err := tx.Put(ctx, "im.conversations", "owner/other-chat", conversation.Conversation{ID: "other-chat", OwnerID: "owner", PeerID: "other"}); err != nil {
			return err
		}
		for i := range 200 {
			id := core.ID(fmt.Sprintf("message-%03d", i))
			m := conversation.Message{ID: id, ConversationID: "other-chat", Content: conversation.Content{Kind: conversation.Text, Text: strings.Repeat("x", 32<<10), AttachmentIDs: []core.ID{"file"}}}
			if err := tx.Put(ctx, "im.messages", persistence.Key("owner", id), m); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	observed := &attachmentAccessStore{Store: db}
	return access{observed}, observed, core.Principal{Kind: core.ActorAssistant, OwnerID: "owner", SubjectID: "assistant"}
}
func TestAttachmentAccessDoesNotLoadBodiesOrRepeatPeerChecks(t *testing.T) {
	a, s, p := attachmentAccessFixture(t)
	if err := a.owns(t.Context(), p, core.Object{Kind: "file", ID: "file"}); core.CodeOf(err) != core.Forbidden {
		t.Fatal(err)
	}
	t.Logf("attachment check loaded %d message bytes and read one peer conversation %d times", s.messageBytes, s.conversationReads)
	if s.messageBytes > 32<<10 || s.conversationReads != 1 {
		t.Fatal("attachment ownership loaded unrelated bodies or repeated peer checks")
	}
}
func BenchmarkAttachmentAccess(b *testing.B) {
	a, s, p := attachmentAccessFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := a.owns(b.Context(), p, core.Object{Kind: "file", ID: "file"}); core.CodeOf(err) != core.Forbidden {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(s.messageBytes)/float64(b.N), "message-bytes/op")
	b.ReportMetric(float64(s.conversationReads)/float64(b.N), "peer-reads/op")
}

func TestAttachmentAccessRechecksPeerAndOwnerBindings(t *testing.T) {
	a, s, p := attachmentAccessFixture(t)
	object := core.Object{Kind: "file", ID: "file"}
	put := func(bucket, key string, value any) {
		t.Helper()
		need(t, s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return tx.Put(ctx, bucket, key, value) }))
	}
	// A foreign account's matching attachment and peer do not confer access.
	put("im.conversations", "foreign/chat", conversation.Conversation{ID: "chat", OwnerID: "foreign", PeerID: p.SubjectID})
	put("im.messages", "foreign/message", conversation.Message{ID: "message", ConversationID: "chat", Content: conversation.Content{Kind: conversation.Text, AttachmentIDs: []core.ID{"file"}}})
	if err := a.owns(t.Context(), p, object); core.CodeOf(err) != core.Forbidden {
		t.Fatal("foreign attachment leaked", err)
	}
	// The permitted reference sorts after all 200 references to the other peer.
	own := conversation.Conversation{ID: "own-chat", OwnerID: p.OwnerID, PeerID: p.SubjectID}
	put("im.conversations", "owner/own-chat", own)
	// Even a foreign message naming this owner's valid conversation is excluded.
	put("im.messages", "foreign/message", conversation.Message{ID: "message", ConversationID: own.ID, Content: conversation.Content{Kind: conversation.Text, AttachmentIDs: []core.ID{"file"}}})
	if err := a.owns(t.Context(), p, object); core.CodeOf(err) != core.Forbidden {
		t.Fatal("foreign owner key bypassed message scope", err)
	}
	put("im.messages", "owner/zzz", conversation.Message{ID: "zzz", ConversationID: own.ID, Content: conversation.Content{Kind: conversation.Text, AttachmentIDs: []core.ID{"unrelated", "file"}}})
	s.conversationReads = 0
	need(t, a.owns(t.Context(), p, object))
	if s.conversationReads != 2 {
		t.Fatal("peer checks did not stop after own reference", s.conversationReads)
	}
	own.PeerID = "other"
	put("im.conversations", "owner/own-chat", own)
	if err := a.owns(t.Context(), p, object); core.CodeOf(err) != core.Forbidden {
		t.Fatal("old peer access cached", err)
	}
	own.PeerID = p.SubjectID
	own.OwnerID = "foreign"
	put("im.conversations", "owner/own-chat", own)
	if err := a.owns(t.Context(), p, object); core.CodeOf(err) != core.Forbidden {
		t.Fatal("conversation owner mismatch accepted", err)
	}
	own.OwnerID = p.OwnerID
	put("im.conversations", "owner/own-chat", own)
	need(t, a.owns(t.Context(), p, object))
	need(t, s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Delete(ctx, "im.conversations", "owner/own-chat")
	}))
	if err := a.owns(t.Context(), p, object); core.CodeOf(err) != core.Forbidden {
		t.Fatal("missing conversation accepted", err)
	}
}
