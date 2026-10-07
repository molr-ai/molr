package model

import (
	"context"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/quota"
	"molr.ai/runtime/tests/testkit"
)

func TestReconciliationSettlesReleasedUnknownHoldOnce(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/model", newContractFixture)
	c := f.Client.(*client)
	f.TransportFailure()
	if _, err := c.Generate(t.Context(), f.Request); err == nil {
		t.Fatal("expected transport failure")
	}
	e := f.Request.Execution
	r, err := f.AwaitReceipt(t.Context(), e.OperationID)
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, c.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "security.reservations", persistence.Key(e.Principal.OwnerID, "reservation"), quota.ModelRecord{OwnerID: e.Principal.OwnerID, CreatedAt: time.Now(), Reservation: quota.Reservation{ID: "reservation", OperationID: e.OperationID, State: "unknown", Reserved: core.Money{Currency: "USD", Micros: 100}}})
	}))
	budgets := &quota.Budgets{Store: c.store}
	c.options.Settle = func(ctx context.Context, e core.Execution, r Receipt) error {
		_, err := budgets.Settle(ctx, e.Principal.OwnerID, quota.SettleRequest{ReservationID: "reservation", OperationID: e.OperationID, ReceiptID: r.ID, Actual: *r.Charged})
		return err
	}
	f.SettleProvider(e.OperationID)
	for range 2 {
		_, err = c.Reconcile(t.Context(), e, r.ID)
		testkit.RequireNoError(t, err)
	}
	usage, err := quota.ModelTotals(t.Context(), c.store, e.Principal.OwnerID, "", time.Time{}, time.Time{})
	testkit.RequireNoError(t, err)
	if usage.Active != 0 || usage.Unknown != 0 || len(usage.Spent) != 1 || usage.Spent[0].Micros != 5 || f.TransportCalls() != 1 {
		t.Fatalf("late settlement duplicated a charge or request: %+v", usage)
	}
}
