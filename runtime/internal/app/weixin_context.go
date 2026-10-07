package app

import (
	"context"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/weixin"
	"molr.ai/runtime/internal/persistence"
)

func weixinContext(ctx context.Context, store persistence.Store, channel *weixin.Service, execution core.Execution) (*agent.ContextDocument, error) {
	if err := (access{store}).execution(ctx, execution); err != nil {
		return nil, err
	}
	a, err := assistant.NewRepository(store).Get(ctx, execution.Principal.OwnerID, execution.AssistantID)
	if err != nil {
		return nil, err
	}
	active, err := channel.Connected(ctx, execution.Principal.OwnerID, a.ConversationID)
	if err != nil || !active {
		return nil, err
	}
	body := "Current chat channel: Weixin. Publish replies as clear plain text; avoid cards because this channel cannot render interactive cards or buttons. If a card is necessary, its title and summary are delivered as text with a Molr link; explain the required action in text. Inbound voice without a transcript is provided as an audio attachment (WAV when decoding succeeds). Read the attachment using the available file tools and an audio-capable model; if content cannot be loaded, explain the limitation rather than inventing a transcript. Quoted messages are marked as quoted context; unavailable or unverified quote selections must not be guessed. Images and files explicitly attached to a published message are sent to Weixin. Receiving an attachment does not mean its contents were read: inspect it using available file tools and model capabilities before describing it. Do not claim delivery or read receipts based on Publish alone. The WebUI remains available for viewing history and managing tasks, files and permissions; chat input is through Weixin."
	return &agent.ContextDocument{Kind: "chat_channel", Source: "runtime:chat_channel", Scope: "assistant:" + string(a.ID), Revision: sourceRevision(body), Content: body}, nil
}
