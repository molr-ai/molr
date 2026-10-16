package app

import (
	"context"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/slack"
	"molr.ai/runtime/internal/persistence"
)

func slackContext(ctx context.Context, store persistence.Store, channel *slack.Service, execution core.Execution) (*agent.ContextDocument, error) {
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
	body := "Current chat channel: Slack private DM. Publish clear text; cards are delivered as title, summary and a Molr link. Explicitly published images and files are uploaded to Slack. Audio and video arrive as file attachments: use file tools and a capable model before describing or transcribing their contents. DM thread parents are quoted from a bounded local cache; unavailable references and failed attachments must not be guessed. All input shares this assistant conversation; do not assume separate thread contexts. Publish does not prove delivery or read receipts. WebUI remains available for history, tasks, files and permissions; chat input is through Slack."

	return &agent.ContextDocument{Kind: "chat_channel", Source: "runtime:chat_channel", Scope: "assistant:" + string(a.ID), Revision: sourceRevision(body), Content: body}, nil
}
