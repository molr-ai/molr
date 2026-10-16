package slack

import (
	"context"
	"molr.ai/runtime/internal/im/channelworker"
	"molr.ai/runtime/internal/persistence"
	"time"
)

func (s *Service) runBindings(ctx context.Context) error {
	return channelworker.Run(ctx, channelworker.Options[Binding]{Store: s.store, Bucket: bindingsBucket, Live: s.options.Live, LiveSignals: s.options.LiveSignals, Connect: s.listen, Step: s.workBinding, Expire: s.expire, Describe: func(b Binding) channelworker.Binding {
		info := channelworker.Binding{ID: b.ID, OwnerID: b.OwnerID, AssistantID: b.AssistantID, ConversationID: b.ConversationID, Connect: b.State != "login_required"}
		info.Work = info.Connect
		if b.ChannelID == "" {
			info.Expires = b.ExpiresAt
		}
		return info
	}})
}
func (s *Service) deliverBinding(ctx context.Context, b Binding) (time.Time, error) {
	var rows []Delivery
	for _, state := range []string{"pending", "waiting", "sending"} {
		head, err := persistence.Select[Delivery](ctx, s.store, deliveriesBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("BindingID", b.ID), persistence.Eq("State", state)}, OrderBy: "Sequence", ThenBy: "Part", Limit: 1})
		if err != nil {
			return time.Time{}, err
		}
		rows = append(rows, head...)
	}
	sortDeliveries(rows)
	if len(rows) == 0 || rows[0].State != "pending" {
		return time.Time{}, nil
	}
	d := rows[0]
	if time.Now().Before(d.NextAttempt) {
		return d.NextAttempt, nil
	}
	err := s.deliver(ctx, d)
	return time.Time{}, err
}

func (s *Service) workBinding(ctx context.Context, b Binding) (time.Time, error) {
	if err := s.receiveBinding(ctx, b); err != nil {
		return time.Time{}, err
	}
	return s.deliverBinding(ctx, b)
}
func (s *Service) receiveBinding(ctx context.Context, b Binding) error {
	if b.ChannelID != "" {
		rows, err := persistence.Select[incomingRecord](ctx, s.store, incomingBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("BindingID", b.ID), persistence.Eq("State", "pending")}, OrderBy: "CreatedAtKey", Limit: 30})
		if err != nil {
			return err
		}
		for _, r := range rows {
			if err = s.ingest(ctx, b, r); err != nil {
				return err
			}
		}
	}
	return nil
}
