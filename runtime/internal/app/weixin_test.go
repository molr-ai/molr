package app

import (
	"context"
	"molr.ai/runtime/internal/modelcatalog"
	"testing"

	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/im/weixin"
	"molr.ai/runtime/internal/persistence"
)

func TestWeixinCompositionEnforcesSingleChatAndOwnership(t *testing.T) {
	ctx := t.Context()
	b, err := Build(ctx, testConfig(t))
	need(t, err)
	defer b.Close()
	owner, _ := ownerSession(t, b)
	binding := resource.Binding{Endpoint: "http://127.0.0.1:19999", AllowedOrigins: []string{"http://127.0.0.1:19999"}, Models: []resource.ModelConfig{{Name: "channel-test", ContextLimit: 32768, SupportsTools: true, Pricing: &resource.ModelPricing{Currency: "USD"}}}}
	r, err := b.Services.Resources.Connect(ctx, owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "provider"}, Kind: resource.Model, Name: "test", Provider: "openai-compatible", Binding: binding})
	need(t, err)
	a, err := b.Services.Assistants.Hire(ctx, owner, assistant.HireRequest{Mutation: core.Mutation{IdempotencyKey: "hire"}, Name: "Weixin", ModelID: modelcatalog.Entries(r, binding)[0].ID, Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}})
	need(t, err)
	st, err := b.weixin.Status(ctx, owner, a.ID)
	need(t, err)
	if st.Channel != "webui" {
		t.Fatal(st)
	}
	forged := owner
	forged.SessionID = "not-a-session"
	if _, err = b.weixin.Status(ctx, forged, a.ID); err == nil {
		t.Fatal("invalid session can read channel")
	}
	// A durable binding is sufficient to deny ordinary ingress, even when its
	// network credential is unavailable. No live Weixin request is made.
	record := weixin.Binding{ID: "binding", OwnerID: owner.OwnerID, AssistantID: a.ID, ConversationID: a.ConversationID, State: "login_required"}
	need(t, b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "weixin.bindings", persistence.Key(owner.OwnerID, a.ConversationID), record)
	}))
	req := conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "send"}, ConversationID: a.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: "web input"}}
	if _, err = b.Services.Conversations.SendUser(ctx, owner, req); core.CodeOf(err) != core.FailedPrecondition {
		t.Fatalf("web input accepted: %v", err)
	}
	if err = b.weixin.Disconnect(ctx, forged, a.ID); err == nil {
		t.Fatal("invalid session can disconnect")
	}
	need(t, b.weixin.Disconnect(ctx, owner, a.ID))
	_, err = b.Services.Conversations.SendUser(ctx, owner, req)
	need(t, err)
}
