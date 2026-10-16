package app

import (
	"context"
	"testing"

	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/im/slack"
	"molr.ai/runtime/internal/im/telegram"
	"molr.ai/runtime/internal/persistence"
)

func TestSlackCompositionEnforcesSingleChatAndOwnership(t *testing.T) {
	ctx := t.Context()
	b, err := Build(ctx, testConfig(t))
	need(t, err)
	defer b.Close()
	owner, _ := ownerSession(t, b)
	binding := resource.Binding{Endpoint: "http://127.0.0.1:19999", AllowedOrigins: []string{"http://127.0.0.1:19999"}, Models: []resource.ModelConfig{{Name: "channel-test", ContextLimit: 32768, SupportsTools: true, Pricing: &resource.ModelPricing{Currency: "USD"}}}}
	r, err := b.Services.Resources.Connect(ctx, owner, resource.ConnectRequest{Mutation: core.Mutation{IdempotencyKey: "provider"}, Kind: resource.Model, Name: "test", Provider: "openai-compatible", Binding: binding})
	need(t, err)
	a, err := b.Services.Assistants.Hire(ctx, owner, assistant.HireRequest{Mutation: core.Mutation{IdempotencyKey: "hire"}, Name: "Slack", ModelID: modelEntries(r, binding)[0].id, Budget: core.AssistantBudget{Limit: core.Money{Currency: "USD"}, Period: "daily"}})
	need(t, err)
	st, err := b.slack.Status(ctx, owner, a.ID)
	need(t, err)
	if st.Channel != "webui" {
		t.Fatal(st)
	}
	forged := owner
	forged.SessionID = "not-a-session"
	if _, err = b.slack.Status(ctx, forged, a.ID); err == nil {
		t.Fatal("invalid session can read channel")
	}
	// A durable binding is sufficient to deny ordinary ingress, even when its
	// network credential is unavailable. No live Slack request is made.
	record := slack.Binding{ID: "binding", OwnerID: owner.OwnerID, AssistantID: a.ID, ConversationID: a.ConversationID, State: "login_required", ChannelID: "DDM"}
	need(t, b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "slack.bindings", persistence.Key(owner.OwnerID, a.ConversationID), record)
	}))
	if _, err = b.weixin.StartLogin(ctx, owner, a.ID); core.CodeOf(err) != core.Conflict {
		t.Fatalf("Weixin can replace Slack: %v", err)
	}
	req := conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "send"}, ConversationID: a.ConversationID, Content: conversation.Content{Kind: conversation.Text, Text: "web input"}}
	if _, err = b.Services.Conversations.SendUser(ctx, owner, req); core.CodeOf(err) != core.FailedPrecondition {
		t.Fatalf("web input accepted: %v", err)
	}

	// The merged router must reject conflicting durable bindings instead of
	// falling back to another channel or accepting an unrelated generation.
	receiver := b.Services.Conversations.(slack.Receiver)
	if _, err = receiver.ReceiveChannel(ctx, owner.OwnerID, "wrong-generation", req); err == nil {
		t.Fatal("wrong channel generation accepted")
	}
	need(t, b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "telegram.bindings", persistence.Key(owner.OwnerID, a.ConversationID), telegram.Binding{ID: "telegram-binding", OwnerID: owner.OwnerID, AssistantID: a.ID, ConversationID: a.ConversationID, State: "connected"})
	}))
	if _, err = receiver.ReceiveChannel(ctx, owner.OwnerID, record.ID, req); core.CodeOf(err) != core.Conflict {
		t.Fatalf("conflicting channels accepted: %v", err)
	}
	need(t, b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Delete(ctx, "telegram.bindings", persistence.Key(owner.OwnerID, a.ConversationID))
	}))
	record.ChannelID = ""
	record.State = "pairing"
	need(t, b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "slack.bindings", persistence.Key(owner.OwnerID, a.ConversationID), record)
	}))
	if _, err = b.weixin.StartLogin(ctx, owner, a.ID); core.CodeOf(err) != core.Conflict {
		t.Fatalf("Weixin replaced pending Slack binding: %v", err)
	}
	if _, err = b.Services.Conversations.SendUser(ctx, owner, req); err != nil {
		t.Fatalf("pairing should keep WebUI input: %v", err)
	}
	if err = b.slack.Disconnect(ctx, forged, a.ID); err == nil {
		t.Fatal("invalid session can disconnect")
	}
	need(t, b.slack.Disconnect(ctx, owner, a.ID))
	_, err = b.Services.Conversations.SendUser(ctx, owner, req)
	need(t, err)
}
