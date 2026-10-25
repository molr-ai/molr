package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/im/dingtalk"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/storage/files"
)

func TestDingtalkMediaAndContextUseLiveBinding(t *testing.T) {
	f := newResourceAccessFixture(t, nil)
	ctx := t.Context()
	a, err := assistant.NewRepository(f.b.store).Get(ctx, f.owner.OwnerID, f.execution.AssistantID)
	need(t, err)
	a.ConversationID = "media-chat"
	need(t, f.b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.assistants", persistence.Key(f.owner.OwnerID, a.ID), a)
	}))
	need(t, conversation.NewRepository(f.b.store).Insert(ctx, conversation.Conversation{ID: a.ConversationID, OwnerID: f.owner.OwnerID, PeerID: a.ID, Version: 1}))
	doc, err := f.b.channelContext(ctx, f.b.store, f.execution)
	need(t, err)
	if len(doc) != 0 {
		t.Fatal("WebUI has Dingtalk restrictions")
	}
	binding := dingtalk.Binding{ID: "media-binding", OwnerID: f.owner.OwnerID, AssistantID: a.ID, ConversationID: a.ConversationID, State: "connected", UserID: "staff"}
	need(t, f.b.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "dingtalk.bindings", persistence.Key(f.owner.OwnerID, a.ConversationID), binding)
	}))
	doc, err = f.b.channelContext(ctx, f.b.store, f.execution)
	need(t, err)
	if len(doc) == 0 || doc[0].Kind != "chat_channel" || !strings.Contains(doc[0].Content, "Interactive cards are unsupported") {
		t.Fatal("channel instructions missing")
	}
	forged := f.execution
	forged.Principal.ExecutionID = "fake"
	if _, err := f.b.channelContext(ctx, f.b.store, forged); err == nil {
		t.Fatal("forged execution accessed channel")
	}
	port := f.b.Services.Files.(files.ChannelStore)
	upload := func(key string, size int64) (files.File, error) {
		return port.UploadChannel(ctx, f.owner.OwnerID, a.ConversationID, binding.ID, files.UploadRequest{Mutation: core.Mutation{IdempotencyKey: key}, Name: "picture.png", MediaType: "image/png", Size: size, Body: bytes.NewReader([]byte("image bytes"))})
	}
	file, err := upload("received", 11)
	need(t, err)
	again, err := upload("received", 11)
	need(t, err)
	if again.ID != file.ID {
		t.Fatal("channel upload not idempotent")
	}
	if _, err := upload("oversize", 1<<40); err == nil {
		t.Fatal("channel bypassed size limit")
	}
	request := conversation.SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "received"}, ConversationID: a.ConversationID, Content: conversation.Content{Kind: conversation.Text, AttachmentIDs: []core.ID{file.ID}}}
	_, err = f.b.Services.Conversations.(dingtalk.Receiver).ReceiveChannel(ctx, f.owner.OwnerID, binding.ID, request)
	need(t, err)
	download, err := f.b.Services.Files.Open(ctx, f.execution.Principal, file.ID)
	need(t, err)
	download.Body.Close()
	ordinary, err := f.b.Services.Files.Upload(ctx, f.owner, files.UploadRequest{Mutation: core.Mutation{IdempotencyKey: "ordinary"}, Name: "private.txt", Size: 1, Body: strings.NewReader("x")})
	need(t, err)
	request.Mutation.IdempotencyKey = "foreign"
	request.Content.AttachmentIDs = []core.ID{ordinary.ID}
	if _, err = f.b.Services.Conversations.(dingtalk.Receiver).ReceiveChannel(ctx, f.owner.OwnerID, binding.ID, request); err == nil {
		t.Fatal("channel attached unrelated owner file")
	}
	need(t, f.b.dingtalk.Disconnect(ctx, f.owner, a.ID))
	doc, err = f.b.channelContext(ctx, f.b.store, f.execution)
	need(t, err)
	if len(doc) != 0 {
		t.Fatal("disconnected channel restriction persisted")
	}
	if _, err := upload("after-disconnect", 11); err == nil {
		t.Fatal("revoked binding uploaded")
	}
}
