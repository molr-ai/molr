package audit

import (
	"context"
	"time"

	"molr.ai/runtime/internal/core"
)

// Repository is private persistence; the service authenticates all read requests.
type Repository interface {
	Append(context.Context, Record) error
	List(context.Context, core.ID, Query) (core.Page[Record], error)
	Prune(context.Context, time.Time, int) (int64, error)
}
