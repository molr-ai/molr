package conversation

import (
	"context"
	"reflect"
	"sort"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type outbox struct{ store persistence.Store }

func OutboxSignal() persistence.SignalKey {
	return persistence.SignalKey{Namespace: "im.outbox", Key: "pending"}
}

func NewOutbox(store persistence.Store) Outbox { return &outbox{store: store} }
func (o *outbox) Append(ctx context.Context, e MessageCreated) error {
	if err := validIDs(e.ID, e.OwnerID, e.ConversationID, e.MessageID, e.SenderID); err != nil {
		return err
	}
	if e.At.IsZero() || (e.Sender != User && e.Sender != Assistant) {
		return core.Invalid("event", "invalid message-created event")
	}
	return o.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var old MessageCreated
		key := persistence.Key(e.OwnerID, e.ID)
		err := tx.Get(ctx, outboxBucket, key, &old)
		if err == nil {
			if reflect.DeepEqual(e, old) {
				return nil
			}
			return core.NewError(core.Conflict, "outbox ID already used")
		}
		if !persistence.Missing(err) {
			return err
		}
		if err := tx.Put(ctx, outboxBucket, key, e); err != nil {
			return err
		}
		return tx.NotifyAfterCommit(ctx, OutboxSignal())
	})
}
func (o *outbox) Pending(ctx context.Context, limit int) (items []MessageCreated, err error) {
	if limit < 1 || limit > 1000 {
		return nil, core.Invalid("limit", "limit must be between 1 and 1000")
	}
	items, err = persistence.Scan[MessageCreated](ctx, o.store, outboxBucket)
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].At.Equal(items[j].At) {
			return items[i].ID < items[j].ID
		}
		return items[i].At.Before(items[j].At)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return
}
func (o *outbox) Acknowledge(ctx context.Context, id core.ID) error {
	if err := id.Validate(); err != nil {
		return err
	}
	return o.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		rows, err := tx.Scan(ctx, outboxBucket)
		if err != nil {
			return err
		}
		for _, row := range rows {
			e, err := persistence.Decode[MessageCreated](row)
			if err != nil {
				return err
			}
			if e.ID == id {
				return tx.Delete(ctx, outboxBucket, row.Key)
			}
		}
		return nil // Repeated delivery acknowledgement is idempotent.
	})
}
