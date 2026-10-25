package app

import (
	"context"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"testing"
)

func TestChatChannelSummaryPreservesBindingAndOwnership(t *testing.T) {
	b, owner, e := taskFixture(t)
	chat := core.ID("channel-summary-chat")
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		var a assistant.Assistant
		if err := tx.Get(ctx, "business.assistants", persistence.Key(owner.OwnerID, e.AssistantID), &a); err != nil {
			return err
		}
		a.ConversationID = chat
		return tx.Put(ctx, "business.assistants", persistence.Key(owner.OwnerID, e.AssistantID), a)
	}))
	read := func(want string) {
		t.Helper()
		value, err := b.channelStatus(t.Context(), owner, e.AssistantID)
		need(t, err)
		if value.(chatChannelStatus).Channel != want {
			t.Fatalf("want %s, got %+v", want, value)
		}
	}
	read("webui")
	for _, name := range []string{"weixin", "telegram", "slack", "dingtalk", "feishu", "qq"} {
		t.Run(name, func(t *testing.T) {
			bucket := name + ".bindings"
			binding := map[string]any{"ID": "binding", "OwnerID": owner.OwnerID, "AssistantID": e.AssistantID, "ConversationID": chat, "State": "reconnecting"}
			put := func() {
				need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
					return tx.Put(ctx, bucket, persistence.Key(owner.OwnerID, chat), binding)
				}))
			}
			put()
			if name == "slack" || name == "dingtalk" || name == "qq" {
				read("webui") // An unconfirmed pairing does not lock web input.
				binding[map[string]string{"slack": "ChannelID", "dingtalk": "UserID", "qq": "OpenID"}[name]] = "confirmed"
				put()
			}
			read(name) // Bound but reconnecting is still an external input channel.
			need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
				return tx.Delete(ctx, bucket, persistence.Key(owner.OwnerID, chat))
			}))
			read("webui")
		})
	}
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "qq.bindings", persistence.Key(owner.OwnerID, chat), map[string]any{"ID": "qq", "OwnerID": owner.OwnerID, "ConversationID": chat, "OpenID": "confirmed"}); err != nil {
			return err
		}
		return tx.Put(ctx, "weixin.bindings", persistence.Key(owner.OwnerID, chat), map[string]any{"ID": "wx", "OwnerID": owner.OwnerID, "ConversationID": chat})
	}))
	read("qq") // Preserve the chat's priority even for inconsistent stored bindings.
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "weixin.bindings", persistence.Key(owner.OwnerID, chat), "invalid binding")
	}))
	if _, err := b.channelStatus(t.Context(), owner, e.AssistantID); err == nil {
		t.Fatal("partial summary hid a storage error")
	}
	forged := owner
	forged.SessionID = "revoked-session"
	if _, err := b.channelStatus(t.Context(), forged, e.AssistantID); err == nil {
		t.Fatal("revoked identity accepted")
	}
	if _, err := b.channelStatus(t.Context(), e.Principal, e.AssistantID); err == nil {
		t.Fatal("assistant actor accepted")
	}
	if _, err := b.channelStatus(t.Context(), owner, "foreign-assistant"); err == nil {
		t.Fatal("unknown assistant accepted")
	}
}
