package qq

import (
	"context"
	"molr.ai/runtime/internal/im/channelworker"
	"molr.ai/runtime/internal/persistence"
	"time"
)

func (s *Service) runBindings(ctx context.Context) error {
	return channelworker.Run(ctx, channelworker.Options[Binding]{Store: s.store, Bucket: bindingsBucket, Live: s.options.Live, LiveSignals: s.options.LiveSignals, Connect: s.listen, Expire: func(ctx context.Context, b Binding) error { return s.expire(ctx, b) }, Step: s.deliverBinding, Describe: func(b Binding) channelworker.Binding {
		info := channelworker.Binding{ID: b.ID, OwnerID: b.OwnerID, AssistantID: b.AssistantID, ConversationID: b.ConversationID, Connect: b.State == "pairing" || b.State == "connecting" || b.State == "connected" || b.State == "reconnecting"}
		info.Work = info.Connect && b.OpenID != ""
		if b.OpenID == "" {
			info.Expires = b.ExpiresAt
		}

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

func (s *Service) expire(ctx context.Context, initial Binding) error {
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		b, err := s.binding(ctx, initial.OwnerID, initial.ConversationID)
		if persistence.Missing(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if b.ID != initial.ID || b.OpenID != "" || time.Now().Before(b.ExpiresAt) {
			return nil
		}
		if err = tx.Delete(ctx, bindingsBucket, persistence.Key(b.OwnerID, b.ConversationID)); err != nil {
			return err
		}
		b.State = "disconnected"
		return s.notify(ctx, b)
	})
}
