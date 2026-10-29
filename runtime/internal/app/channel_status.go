package app

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/channelstate"
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

// management reads every binding in one transaction. Pairing reservations remain
// visible, but delivery history and credentials never enter this response.
func (s channelSummary) management(ctx context.Context, p core.Principal, id core.ID, readers map[string]func(context.Context, core.Principal, core.ID) (channelstate.Summary, error)) (map[string]channelstate.Summary, error) {
	result := make(map[string]channelstate.Summary, len(readers))
	err := s.access.store.Read(ctx, func(ctx context.Context, _ persistence.Tx) error {
		if _, err := s.access.target(ctx, p, id, false); err != nil {
			return err
		}
		for _, name := range []string{"weixin", "qq", "feishu", "dingtalk", "telegram", "slack"} {
			value, err := readers[name](ctx, p, id)
			if err != nil {
				return err
			}
			result[name] = value
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
