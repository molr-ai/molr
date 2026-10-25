package channelstate

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/channelworker"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"path/filepath"
	"testing"
)

func TestEnvelopeBindsIdentityAndRejectsDamage(t *testing.T) {
	block, err := aes.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	c, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("synthetic-token")
	a, err := Seal(c, raw, "channel/owner/binding/app")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Seal(c, raw, "channel/owner/binding/app")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("nonce reused")
	}
	opened, err := Open(c, a, "channel/owner/binding/app", "short envelope")
	if err != nil || !bytes.Equal(opened, raw) {
		t.Fatal(err)
	}
	clear(opened)
	for _, aad := range []string{"other/owner/binding/app", "channel/other/binding/app", "channel/owner/other/app", "channel/owner/binding/other"} {
		if _, err = Open(c, a, aad, "short envelope"); err == nil {
			t.Fatal("cross-identity credential accepted", aad)
		}
	}
	damaged := append([]byte(nil), a...)
	damaged[len(damaged)-1] ^= 1
	if _, err = Open(c, damaged, "channel/owner/binding/app", "short envelope"); err == nil {
		t.Fatal("tampered credential accepted")
	}
	if _, err = Open(c, a[:c.NonceSize()-1], "channel/owner/binding/app", "short envelope"); core.CodeOf(err) != core.Internal {
		t.Fatal(err)
	}
}
func TestConnectionNotifyPreservesScopeRecoveryAndCommitOrder(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := t.Context()
	c := Connection{Channel: "fixture", Bucket: "fixture.bindings", State: "reconnecting", OwnerID: "owner", AssistantID: "assistant", ConversationID: "chat", Attention: true}
	if err = Notify(ctx, db, c); err != nil {
		t.Fatal(err)
	}
	pending, err := persistence.Load[channelworker.Binding](ctx, db, c.Bucket+".pending", persistence.Key(c.OwnerID, c.ConversationID))
	if err != nil || pending.ConversationID != c.ConversationID {
		t.Fatal(pending, err)
	}
	if err = SyncNotice(ctx, db, c); err != nil {
		t.Fatal(err)
	}
	notices, err := persistence.Scan[notification.Notice](ctx, db, "notifications.records")
	if err != nil || len(notices) != 1 || notices[0].Kind != "fixture.connection" || notices[0].OwnerID != "owner" {
		t.Fatal(notices, err)
	}
	events, err := realtime.NewStore(db).Replay(ctx, c.OwnerID, realtime.Query{Topics: []string{"fixture.changed"}})
	if err != nil || len(events.Items) != 1 || events.Items[0].ConversationID != "chat" {
		t.Fatal(events, err)
	}
	c.Attention = false
	c.State = "connected"
	if err = Notify(ctx, db, c); err != nil {
		t.Fatal(err)
	}
	notices, err = persistence.Scan[notification.Notice](ctx, db, "notifications.records")
	if err != nil || len(notices) != 1 || notices[0].ResolvedAt == nil {
		t.Fatal(notices, err)
	}
	// Invalid notice identity fails after the worker commit and before an SSE event.
	c.ConversationID = "failed-chat"
	c.AssistantID = ""
	if err = Notify(ctx, db, c); err == nil {
		t.Fatal("invalid notice accepted")
	}
	if _, err = persistence.Load[channelworker.Binding](ctx, db, c.Bucket+".pending", persistence.Key(c.OwnerID, c.ConversationID)); err != nil {
		t.Fatal("worker commit rolled back", err)
	}
	rows, err := realtime.NewStore(db).Replay(ctx, c.OwnerID, realtime.Query{Topics: []string{"fixture.changed"}})
	if err != nil || len(rows.Items) != 2 {
		t.Fatal("failed notice emitted event", rows, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err = Notify(canceled, db, c); err == nil {
		t.Fatal("canceled notify succeeded")
	}
}
