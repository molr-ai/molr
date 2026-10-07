package policy

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
)

func TestAssistantBudgetSharesTasksAndRetainsPendingAcrossReset(t *testing.T) {
	ctx := t.Context()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "budget.db"))
	testkit.RequireNoError(t, err)
	defer db.Close()
	now := time.Date(2026, 9, 24, 15, 59, 0, 0, time.UTC)
	r := &repository{store: db, now: func() time.Time { return now }}
	testkit.RequireNoError(t, db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "business.settings", "owner", struct{ Timezone string }{"Asia/Shanghai"}); err != nil {
			return err
		}
		return tx.Put(ctx, "business.assistants", "owner/assistant", struct {
			OwnerID core.ID
			Budget  core.AssistantBudget
		}{"owner", core.AssistantBudget{Limit: money(100), Period: "daily"}})
	}))
	reserve := func(id, task core.ID, amount int64) (Reservation, error) {
		return r.Reserve(ctx, ReserveRequest{Execution: core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: "assistant", TaskID: task, OperationID: id}, Estimate: money(amount)})
	}
	first, err := reserve("chat", "", 40)
	testkit.RequireNoError(t, err)
	_, err = r.Settle(ctx, "owner", SettleRequest{ReservationID: first.ID, OperationID: "chat", ReceiptID: "receipt1", Actual: money(40)})
	testkit.RequireNoError(t, err)
	pending, err := reserve("task-call", "task", 60)
	testkit.RequireNoError(t, err)
	_, err = reserve("another-task", "task2", 1)
	testkit.RequireCode(t, err, core.BudgetExceeded)
	now = now.Add(time.Minute)
	usage, err := r.Usage(ctx, "owner", UsageQuery{AssistantID: "assistant", CurrentPeriod: true})
	testkit.RequireNoError(t, err)
	if usage.Spent.Micros != 0 || usage.Reserved.Micros != 60 || usage.PeriodStart.UTC().Hour() != 16 {
		t.Fatalf("wrong reset usage: %+v", usage)
	}
	_, err = reserve("too-much", "", 41)
	testkit.RequireCode(t, err, core.BudgetExceeded)
	_, err = reserve("new-day", "", 40)
	testkit.RequireNoError(t, err)
	_, err = r.Settle(ctx, "owner", SettleRequest{ReservationID: pending.ID, OperationID: "task-call", ReceiptID: "receipt2", Actual: money(50)})
	testkit.RequireNoError(t, err)
	usage, err = r.Usage(ctx, "owner", UsageQuery{AssistantID: "assistant", CurrentPeriod: true})
	testkit.RequireNoError(t, err)
	if usage.Spent.Micros != 0 || usage.Reserved.Micros != 40 {
		t.Fatalf("late settlement belongs to original period: %+v", usage)
	}
	history, err := r.Usage(ctx, "owner", UsageQuery{AssistantID: "assistant"})
	testkit.RequireNoError(t, err)
	if history.Spent.Micros != 90 {
		t.Fatalf("reset lost history: %+v", history)
	}
	// Lowering a cap does not erase current holds.
	testkit.RequireNoError(t, db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.assistants", "owner/assistant", struct {
			OwnerID core.ID
			Budget  core.AssistantBudget
		}{"owner", core.AssistantBudget{Limit: money(30), Period: "daily"}})
	}))
	_, err = reserve("lowered", "", 1)
	testkit.RequireCode(t, err, core.BudgetExceeded)
}

func TestBudgetCalendarWeekTimezoneAndDST(t *testing.T) {
	for _, tc := range []struct{ period, at, start, end string }{
		{"daily", "2026-03-08T15:00:00Z", "2026-03-08T05:00:00Z", "2026-03-09T04:00:00Z"},
		{"weekly", "2026-11-01T15:00:00Z", "2026-10-26T04:00:00Z", "2026-11-02T05:00:00Z"},
		{"weekly", "2026-11-02T05:00:00Z", "2026-11-02T05:00:00Z", "2026-11-09T05:00:00Z"},
	} {
		t.Run(tc.at, func(t *testing.T) {
			db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "calendar.db"))
			testkit.RequireNoError(t, err)
			defer db.Close()
			at, _ := time.Parse(time.RFC3339, tc.at)
			r := &repository{store: db, now: func() time.Time { return at }}
			testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
				if err := tx.Put(ctx, "business.settings", "owner", struct{ Timezone string }{"America/New_York"}); err != nil {
					return err
				}
				return tx.Put(ctx, "business.assistants", "owner/assistant", struct {
					OwnerID core.ID
					Budget  core.AssistantBudget
				}{"owner", core.AssistantBudget{Limit: money(100), Period: tc.period}})
			}))
			spec, err := r.Usage(t.Context(), "owner", UsageQuery{AssistantID: "assistant", CurrentPeriod: true})
			testkit.RequireNoError(t, err)
			if spec.PeriodStart.UTC().Format(time.RFC3339) != tc.start || spec.PeriodEnd.UTC().Format(time.RFC3339) != tc.end {
				t.Fatalf("wrong calendar: %+v", spec)
			}
		})
	}
}
