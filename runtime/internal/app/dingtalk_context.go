package app

import (
	"context"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/dingtalk"
	"molr.ai/runtime/internal/persistence"
)

func dingtalkContext(ctx context.Context, store persistence.Store, channel *dingtalk.Service, execution core.Execution) (*agent.ContextDocument, error) {
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
	body := "Current chat channel: DingTalk private chat. Publish replies as text or Markdown. Interactive cards are unsupported; publish clear text and direct users to Molr for approvals and management. Any necessary Molr card is delivered as a text summary and a login-required link. Only explicitly published attachments are sent, as downloadable files (including images). Incoming images and files are attachments; inspect them before describing their contents. Voice uses DingTalk's transcript when provided, otherwise an audio attachment; understanding depends on format and model support. Never invent unread or unsupported content. Do not claim delivery or read receipts based on Publish alone. WebUI retains history, tasks, files and permissions; chat input is through DingTalk."
	return &agent.ContextDocument{Kind: "chat_channel", Source: "runtime:chat_channel", Scope: "assistant:" + string(a.ID), Revision: sourceRevision(body), Content: body}, nil
}
