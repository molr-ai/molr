// Package realtime owns authorized delivery and replay without controlling Agent execution.
package realtime

import (
	"context"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// Visible contains safe references, never prompts, credentials or raw tool output.
// Sequence is assigned in commit order in the owner's stream.
type Visible struct {
	ConversationID core.ID `json:",omitempty"`
	ID             core.ID
	OwnerID        core.ID
	Sequence       uint64
	Topic          string
	Object         core.Object
	Version        core.Version
	CreatedAt      time.Time
}
type Query struct {
	After  string
	Topics []string
	Page   core.PageRequest
}

// Feed checks authorization on replay and while streaming. Expired/revoked
// credentials terminate Next with an authentication error. A cursor is not a grant.
type Feed interface {
	List(context.Context, core.Principal, Query) (core.Page[Visible], error)
	Subscribe(context.Context, core.Principal, Query) (Subscription, error)
}
type Subscription interface {
	// StartCursor is fixed before Subscribe returns, including on an empty stream.
	StartCursor() string
	Next(context.Context) (Visible, error)
	Close() error
}

// Store is trusted-internal. Append participates in the business transaction.
// Each client has its own cursor; replay never consumes another client's events.
type Store interface {
	Head(context.Context, core.ID, []string) (string, error)
	Watch(core.ID) (persistence.Watch, error)
	Append(context.Context, Visible) error
	Replay(context.Context, core.ID, Query) (core.Page[Visible], error)
}
