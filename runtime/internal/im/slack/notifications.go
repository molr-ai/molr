package slack

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/persistence"
)

func (s *Service) saveDelivery(ctx context.Context, tx persistence.Tx, d Delivery) error {
	if err := tx.Put(ctx, deliveriesBucket, persistence.Key(d.OwnerID, d.ID), d); err != nil {
		return err
	}
	// Keep an existing warning during the retry; only an outcome can resolve it.
	if d.State == "sending" || d.State == "pending" && d.Error == "" {
		return nil
	}
	return notification.Sync(ctx, s.store, d.OwnerID, notification.Input{Kind: "slack.delivery", Code: d.State + ":" + d.Error, Source: core.Object{Kind: "slack_delivery", ID: d.ID}, AssistantID: d.AssistantID}, d.State != "accepted" && d.State != "canceled")
}
