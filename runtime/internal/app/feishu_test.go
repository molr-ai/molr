package app

import (
	"context"
	"molr.ai/runtime/internal/im/dingtalk"
	"molr.ai/runtime/internal/im/slack"
	"molr.ai/runtime/internal/im/telegram"
	"molr.ai/runtime/internal/im/weixin"
	"molr.ai/runtime/internal/modelcatalog"
	"testing"
	"time"

	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/im/feishu"
	"molr.ai/runtime/internal/persistence"
)

func TestFeishuCompositionEnforcesSingleChatAndOwnership(t *testing.T) {
	ctx := t.Context()
	b, err := Build(ctx, testConfig(t))
	need(t, err)
	defer b.Close()
	owner, _ := ownerSession(t, b)
	binding := resource.Binding{Endpoint: "http://127.0.0.1:19999", AllowedOrigins: []string{"http://127.0.0.1:19999"}, Models: []resource.ModelConfig{{Name: "channel-test", ContextLimit: 32768, SupportsTools: true, Pricing: &resource.ModelPricing{Currency: "USD"}}}}
	r, err := b.Services.Resources.Connect(ctx, owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "provider"}, Kind: resource.Model, Name: "test", Provider: "openai-compatible", Binding: binding})
	need(t, err)
	a, err := b.Services.Assistants.Hire(ctx, owner, assistant.HireRequest{Mutation: core.Mutation{IdempotencyKey: "hire"}, Name: "Feishu", ModelID: modelcatalog.Entries(r, binding)[0].ID, Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}})
	need(t, err)
	st, err := b.feishu.Status(ctx, owner, a.ID)
	need(t, err)
	if st.Channel != "webui" {
		t.Fatal(st)
	}
	forged := owner
	forged.SessionID = "not-a-session"
	if _, err = b.feishu.Status(ctx, forged, a.ID); err == nil {
		t.Fatal("invalid session can read channel")
	}
	// A durable binding is sufficient to deny ordinary ingress, even when its
	// network credential is unavailable. No live Feishu request is made.
	record := feishu.Binding{ID: "binding", OwnerID: owner.OwnerID, AssistantID: a.ID, ConversationID: a.ConversationID, State: "credentials_required"}
	need(t, b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "feishu.bindings", persistence.Key(owner.OwnerID, a.ConversationID), record)
	}))
	if _, err = b.weixin.StartLogin(ctx, owner, a.ID); core.CodeOf(err) != core.FailedPrecondition {
		t.Fatalf("Weixin replaced Feishu: %v", err)
	}
	if _, err = b.dingtalk.Start(ctx, owner, dingtalk.ConnectRequest{AssistantID: a.ID, ClientID: "fixture-app", ClientSecret: "fixture-secret"}); core.CodeOf(err) != core.FailedPrecondition {
		t.Fatalf("DingTalk replaced Feishu: %v", err)
	}
	req := conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "send"}, ConversationID: a.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: "web input"}}
	if _, err = b.Services.Conversations.SendUser(ctx, owner, req); core.CodeOf(err) != core.FailedPrecondition {
		t.Fatalf("web input accepted: %v", err)
	}
	if err = b.feishu.Disconnect(ctx, forged, a.ID); err == nil {
		t.Fatal("invalid session can disconnect")
	}
	need(t, b.feishu.Disconnect(ctx, owner, a.ID))
	for _, tc := range []struct {
		bucket string
		value  any
	}{
		{"weixin.bindings", weixin.Binding{ID: "weixin-binding", OwnerID: owner.OwnerID, AssistantID: a.ID, ConversationID: a.ConversationID, State: "connected"}},
		{"telegram.bindings", telegram.Binding{ID: "telegram-binding", OwnerID: owner.OwnerID, AssistantID: a.ID, ConversationID: a.ConversationID, State: "connected"}},
		{"slack.bindings", slack.Binding{ID: "slack-binding", OwnerID: owner.OwnerID, AssistantID: a.ID, ConversationID: a.ConversationID, State: "pairing"}},
		{"dingtalk.bindings", dingtalk.Binding{ID: "ding-binding", OwnerID: owner.OwnerID, AssistantID: a.ID, ConversationID: a.ConversationID, State: "pairing", ExpiresAt: time.Now().Add(time.Minute)}},
	} {
		need(t, b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, tc.bucket, persistence.Key(owner.OwnerID, a.ConversationID), tc.value)
		}))
		if _, err = b.feishu.StartLogin(ctx, owner, a.ID); err == nil {
			t.Fatalf("Feishu ignored %s", tc.bucket)
		}
		need(t, b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return tx.Delete(ctx, tc.bucket, persistence.Key(owner.OwnerID, a.ConversationID))
		}))
	}
	_, err = b.Services.Conversations.SendUser(ctx, owner, req)
	need(t, err)
}
