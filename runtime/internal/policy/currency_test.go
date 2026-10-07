package policy

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/pricing"
	"molr.ai/runtime/tests/testkit"
)

func TestMixedCurrenciesKeepNativeReceiptsAndBudgetQuote(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "currency.db"))
	testkit.RequireNoError(t, err)
	defer db.Close()
	ctx := t.Context()
	rates := pricing.ExchangeSnapshot{Version: "test-7", Units: map[string]int64{"USD": 1, "CNY": 7}}
	repo := &repository{store: db, now: time.Now, exchange: func() pricing.ExchangeSnapshot { return rates }}
	set := func(currency string, limit core.Money) {
		t.Helper()
		testkit.RequireNoError(t, db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			if err := tx.Put(ctx, "business.settings", "owner", struct{ Currency string }{currency}); err != nil {
				return err
			}
			return tx.Put(ctx, "business.assistants", "owner/assistant", struct {
				OwnerID core.ID
				Budget  core.AssistantBudget
			}{"owner", core.AssistantBudget{Limit: limit, Period: "daily"}})
		}))
	}
	set("CNY", core.Money{Currency: "CNY", Micros: 7000000})
	reserve := func(id core.ID, amount core.Money) (Reservation, error) {
		return repo.Reserve(ctx, ReserveRequest{Execution: core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: "assistant", OperationID: id}, Estimate: amount})
	}
	usd, err := reserve("usd", core.Money{Currency: "USD", Micros: 1000000})
	testkit.RequireNoError(t, err)
	if usd.Reserved.Currency != "USD" || usd.BudgetEstimate.Currency != "CNY" || usd.BudgetEstimate.Micros != 7000000 || usd.Exchange.Version != "test-7" {
		t.Fatalf("lost native denomination or decision quote: %+v", usd)
	}
	_, err = reserve("blocked", core.Money{Currency: "CNY", Micros: 1})
	testkit.RequireCode(t, err, core.BudgetExceeded)
	_, err = repo.Settle(ctx, "owner", SettleRequest{ReservationID: usd.ID, OperationID: "usd", ReceiptID: "usd-receipt", Actual: core.Money{Currency: "USD", Micros: 500000}})
	testkit.RequireNoError(t, err)
	cny, err := reserve("cny", core.Money{Currency: "CNY", Micros: 3500000})
	testkit.RequireNoError(t, err)
	_, err = repo.Settle(ctx, "owner", SettleRequest{ReservationID: cny.ID, OperationID: "cny", ReceiptID: "cny-receipt", Actual: core.Money{Currency: "CNY", Micros: 1000000}})
	testkit.RequireNoError(t, err)
	usage, err := repo.Usage(ctx, "owner", UsageQuery{AssistantID: "assistant", CurrentPeriod: true})
	testkit.RequireNoError(t, err)
	if usage.Spent != (core.Money{Currency: "CNY", Micros: 4500000}) || len(usage.OriginalSpent) != 2 {
		t.Fatalf("invalid multi-currency totals: %+v", usage)
	}
	set("USD", core.Money{Currency: "USD", Micros: 1000000})
	usage, err = repo.Usage(ctx, "owner", UsageQuery{AssistantID: "assistant", CurrentPeriod: true})
	testkit.RequireNoError(t, err)
	if usage.Spent != (core.Money{Currency: "USD", Micros: 642858}) || usage.OriginalSpent[0] != (core.Money{Currency: "CNY", Micros: 1000000}) || usage.OriginalSpent[1] != (core.Money{Currency: "USD", Micros: 500000}) {
		t.Fatalf("preference change rewrote native amounts: %+v", usage)
	}
	rates = pricing.ExchangeSnapshot{Version: "test-8", Units: map[string]int64{"USD": 1, "CNY": 8}}
	_, err = reserve("new", core.Money{Currency: "USD", Micros: 375000})
	testkit.RequireNoError(t, err)
	stored, err := persistence.Load[reservationRecord](ctx, db, "security.reservations", persistence.Key("owner", usd.ID))
	testkit.RequireNoError(t, err)
	if stored.Reservation.Exchange.Version != "test-7" || stored.Reservation.Exchange.Units["CNY"] != 7 || stored.Reservation.Settled.Currency != "USD" {
		t.Fatal("rate update rewrote original evidence")
	}
}
