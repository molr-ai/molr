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
		if value.Channel != want {
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

func TestChannelManagementSummaryPreservesPairingAndFailsClosed(t *testing.T) {
	b, owner, execution := taskFixture(t)
	a, err := b.Services.Assistants.Get(t.Context(), owner, execution.AssistantID)
	need(t, err)
	key := persistence.Key(owner.OwnerID, a.ConversationID)
	put := func(channel, state string, confirmed bool) {
		t.Helper()
		value := map[string]any{"ID": "binding", "OwnerID": owner.OwnerID, "AssistantID": a.ID, "ConversationID": a.ConversationID, "State": state, "ExpiresAt": "2099-01-01T00:00:00Z"}
		if confirmed {
			value[map[string]string{"qq": "OpenID", "dingtalk": "UserID", "slack": "ChannelID"}[channel]] = "confirmed"
		}
		need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, channel+".bindings", key, value)
		}))
	}
	for _, channel := range []string{"qq", "dingtalk", "slack"} {
		put(channel, "pairing", false)
		values, err := b.channelManagement(t.Context(), owner, a.ID)
		need(t, err)
		if len(values) != 6 || values[channel].Channel != "webui" || values[channel].State != "pairing" {
			t.Fatalf("pairing lost: %+v", values)
		}
		put(channel, "reconnecting", true)
		values, err = b.channelManagement(t.Context(), owner, a.ID)
		need(t, err)
		if values[channel].Channel != channel || values[channel].State != "reconnecting" {
			t.Fatalf("binding lost: %+v", values)
		}
	}
	forged := owner
	forged.SessionID = "revoked"
	if values, err := b.channelManagement(t.Context(), forged, a.ID); err == nil || values != nil {
		t.Fatalf("identity accepted: %+v %v", values, err)
	}
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "weixin.bindings", key, "invalid binding")
	}))
	if values, err := b.channelManagement(t.Context(), owner, a.ID); err == nil || values != nil {
		t.Fatalf("partial summary returned: %+v %v", values, err)
	}
}
