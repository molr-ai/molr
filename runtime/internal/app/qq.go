package app

import (
	"context"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/qq"
	"molr.ai/runtime/internal/persistence"
)

func qqContext(ctx context.Context, store persistence.Store, channel *qq.Service, execution core.Execution) (*agent.ContextDocument, error) {
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
	body := "Current chat channel: QQ private chat. Publish user-facing replies as plain text using conversation_publish. This channel currently receives text only; unsupported attachments are explicitly marked unread. Do not infer their contents. Group chats are unavailable. Keep replies concise: QQ limits passive replies per inbound message. Proactive notifications depend on the recipient allowing them. Published attachments and cards are represented by a Molr link; explain any required action in text. The WebUI remains available for history, files, tasks and permissions. Publish queues delivery and does not prove remote delivery or reading."
	return &agent.ContextDocument{Kind: "chat_channel", Source: "runtime:chat_channel", Scope: "assistant:" + string(a.ID), Revision: sourceRevision(body), Content: body}, nil
}
