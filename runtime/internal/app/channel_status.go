package app

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
)

type namedChannel struct {
	name    string
	channel chatChannel
}
type channelSummary struct {
	access   channelAccess
	channels []namedChannel
}

// Read one authorized snapshot of confirmed bindings. Delivery history and
// pairing details belong to the channel dialogs, not the chat input gate.
func (s channelSummary) read(ctx context.Context, p core.Principal, id core.ID) (conversation.ChannelStatus, error) {
	result := conversation.ChannelStatus{Channel: "webui"}
	err := s.access.store.Read(ctx, func(ctx context.Context, _ persistence.Tx) error {
		target, err := s.access.target(ctx, p, id, false)
		if err != nil {
			return err
		}
		for _, item := range s.channels {
			bound, err := item.channel.Connected(ctx, p.OwnerID, target.ConversationID)
			if err != nil {
				return err
			}
			if bound && result.Channel == "webui" {
				result.Channel = item.name
			}
		}
		return nil
	})
	return result, err
}
