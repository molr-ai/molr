package channelstate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/channelworker"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/persistence"
	"time"
)

type Connection struct {
	Channel, Bucket, State               string
	OwnerID, AssistantID, ConversationID core.ID
	Attention                            bool
}

func SyncNotice(ctx context.Context, store persistence.Store, c Connection) error {
	return notification.Sync(ctx, store, c.OwnerID, notification.Input{Kind: c.Channel + ".connection", Code: c.State, Source: core.Object{Kind: "assistant", ID: c.AssistantID}, AssistantID: c.AssistantID}, c.Attention)
}

// Notify preserves the worker -> notice -> visible-event commit order. A failed
// stage stops subsequent writes; it does not erase a previously committed stage.
func Notify(ctx context.Context, store persistence.Store, c Connection) error {
	if err := store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return channelworker.Changed(ctx, tx, c.Bucket, c.OwnerID, c.ConversationID)
	}); err != nil {
		return err
	}
	if err := SyncNotice(ctx, store, c); err != nil {
		return err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic(err)
	}
	return realtime.NewStore(store).Append(ctx, realtime.Visible{ID: core.ID(hex.EncodeToString(id[:])), OwnerID: c.OwnerID, Topic: c.Channel + ".changed", Object: core.Object{Kind: "assistant", ID: c.AssistantID}, ConversationID: c.ConversationID, Version: 1, CreatedAt: time.Now().UTC()})
}
