package quota

import (
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/pricing"
	"time"
)

// Only reserved holds allowance. Unknown ends the local hold without asserting
// no charge; unlike released, it still accepts a later settlement.
type Reservation struct {
	BudgetEstimate *core.Money
	Exchange       *pricing.ExchangeSnapshot
	ID             core.ID
	OperationID    core.ID
	TaskID         core.ID
	Reserved       core.Money
	Settled        *core.Money
	State          string
}
type ReserveRequest struct {
	ModelID   core.ID
	Execution core.Execution
	Estimate  core.Money
}
type SettleRequest struct {
	ReservationID core.ID
	OperationID   core.ID
	ReceiptID     core.ID
	Actual        core.Money
}
type UsageQuery struct {
	CurrentPeriod bool
	AssistantID   core.ID
	TaskID        core.ID
	From          time.Time
	Until         time.Time
}
type Usage struct {
	OriginalSpent, OriginalReserved []core.Money
	Exchange                        *pricing.ExchangeSnapshot
	Limit                           *core.Money
	PeriodStart, PeriodEnd          *time.Time
	Spent                           core.Money
	Reserved                        core.Money
	UnknownOperations               []core.ID
}
