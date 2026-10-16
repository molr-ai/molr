package feishu

import (
	"context"
	"molr.ai/runtime/internal/im/channelworker"
	"molr.ai/runtime/internal/persistence"
	"time"
)

func (s *Service) runBindings(ctx context.Context) error {
	return channelworker.Run(ctx, channelworker.Options[Binding]{Store: s.store, Bucket: bindingsBucket, Live: s.options.Live, LiveSignals: s.options.LiveSignals, Connect: s.listen, Step: s.deliverBinding, Describe: func(b Binding) channelworker.Binding {
		info := channelworker.Binding{ID: b.ID, OwnerID: b.OwnerID, AssistantID: b.AssistantID, ConversationID: b.ConversationID, Connect: b.State == "connecting" || b.State == "connected" || b.State == "reconnecting"}
		info.Work = info.Connect

		return info
	}})
}
func (s *Service) deliverBinding(ctx context.Context, b Binding) (time.Time, error) {

	rows, err := persistence.Select[Delivery](ctx, s.store, deliveriesBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("BindingID", b.ID), persistence.Eq("State", "pending")}, OrderBy: "Sequence", ThenBy: "Part", Limit: 1})
	if err != nil {
		return time.Time{}, err
	}
	if len(rows) == 0 || rows[0].State != "pending" {
		return time.Time{}, nil
	}
	d := rows[0]
	if time.Now().Before(d.NextAttempt) {
		return d.NextAttempt, nil
	}
	err = s.deliver(ctx, d)
	return time.Time{}, err
}
