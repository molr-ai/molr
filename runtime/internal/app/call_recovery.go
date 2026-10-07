package app

import (
	"context"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/quota"
)

// Called only after acquiring the Runtime's exclusive lock and before starting
// workers. No local calls from the previous process can still be running here.
// Background shell execution is recovered separately by the shell task manager.
func recoverCallUsage(ctx context.Context, store persistence.Store) error {
	return store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		budgets := &quota.Budgets{Store: tx.Store()}
		rows, err := persistence.Select[quota.ModelRecord](ctx, tx.Store(), "security.reservations", persistence.Query{Unordered: true, Any: [][]persistence.Filter{
			{persistence.Eq("Reservation.State", "reserved")},
			{persistence.Eq("Reservation.State", "unknown")},
		}})
		if err != nil {
			return err
		}
		for _, row := range rows {
			receipt, err := model.NewReceipts(tx.Store()).ByOperation(ctx, row.OwnerID, row.Reservation.OperationID)
			if err != nil && !persistence.Missing(err) {
				return err
			}
			state := "unknown"
			// Missing evidence must never turn an already-unknown charge into a
			// proven no-charge release (for example, when a receipt index is absent).
			if persistence.Missing(err) && row.Reservation.State == "reserved" || receipt.Failure != nil && receipt.Failure.NoCharge {
				state = "released"
			} else if receipt.Charged != nil {
				_, err = budgets.Settle(ctx, row.OwnerID, quota.SettleRequest{ReservationID: row.Reservation.ID, OperationID: row.Reservation.OperationID, ReceiptID: receipt.ID, Actual: *receipt.Charged})
				if err != nil {
					return err
				}
				continue
			}
			if err := budgets.SetReservationState(ctx, row.OwnerID, row.Reservation.ID, state); err != nil {
				return err
			}
		}
		ops, err := persistence.Select[operation.Operation](ctx, tx.Store(), "agent.operations", persistence.Query{Unordered: true, Where: []persistence.Filter{persistence.Eq("State", operation.Running)}})
		if err != nil {
			return err
		}
		repo := operation.NewRepository(tx.Store())
		for _, op := range ops {
			version := op.Version
			op.State = operation.Unknown
			op.Version++
			op.UpdatedAt = time.Now().UTC()
			if err := repo.Save(ctx, op, version); err != nil {
				return err
			}
		}
		return nil
	})
}
