package model

import (
	"context"

	"molr.ai/runtime/internal/core"
)

// Receipts persists provider evidence even when a caller stops consuming a
// stream. A late result may resolve an unknown receipt, never erase known spend.
// Owner and operation identity must match the original model operation.
type Receipts interface {
	Get(context.Context, core.ID, core.ID) (Receipt, error)
	ByOperation(context.Context, core.ID, core.ID) (Receipt, error)
	Record(context.Context, core.ID, Receipt) error
}
