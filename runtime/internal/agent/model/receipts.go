package model

import (
	"context"
	"reflect"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

const receiptsBucket = "agent.model_receipts"

type receipts struct{ store persistence.Store }

func NewReceipts(store persistence.Store) Receipts { return &receipts{store} }
func (r *receipts) Get(ctx context.Context, owner, id core.ID) (Receipt, error) {
	return persistence.Load[Receipt](ctx, r.store, receiptsBucket, persistence.Key(owner, id))
}
func (r *receipts) ByOperation(ctx context.Context, owner, id core.ID) (Receipt, error) {
	index, err := persistence.Load[struct{ ID core.ID }](ctx, r.store, "agent.model_receipt_index", persistence.Key(owner, id))
	if err != nil {
		return Receipt{}, err
	}
	return r.Get(ctx, owner, index.ID)
}
func (r *receipts) Record(ctx context.Context, owner core.ID, value Receipt) error {
	value.CreatedAt = value.CreatedAt.UTC()
	for _, id := range []core.ID{owner, value.ID, value.OperationID} {
		if err := id.Validate(); err != nil {
			return err
		}
	}
	if value.CreatedAt.IsZero() || value.Usage.InputTokens < 0 || value.Usage.OutputTokens < 0 || value.Usage.CachedTokens < 0 || value.Usage.CachedTokens > value.Usage.InputTokens {
		return core.Invalid("receipt", "invalid receipt")
	}
	if value.Charged != nil {
		if err := value.Charged.Validate(); err != nil {
			return err
		}
	}
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		existing, err := r.Get(ctx, owner, value.ID)
		if err == nil {
			if existing.OperationID != value.OperationID {
				return core.NewError(core.Conflict, "receipt operation is immutable")
			}
			if existing.OutcomeKnown {
				if !value.OutcomeKnown {
					return nil
				}
				if !reflect.DeepEqual(existing, value) {
					return core.NewError(core.Conflict, "known receipt evidence cannot be replaced")
				}
				return nil
			}
		} else if !persistence.Missing(err) {
			return err
		}
		other, err := r.ByOperation(ctx, owner, value.OperationID)
		if err == nil && other.ID != value.ID {
			return core.NewError(core.Conflict, "operation already has a receipt")
		}
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if err = tx.Put(ctx, receiptsBucket, persistence.Key(owner, value.ID), value); err != nil {
			return err
		}
		return tx.Put(ctx, "agent.model_receipt_index", persistence.Key(owner, value.OperationID), struct{ ID core.ID }{value.ID})
	})
}
