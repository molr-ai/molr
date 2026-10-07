package app

import (
	"context"
	"testing"
	"time"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/quota"
)

func TestCallRecoveryReleasesHoldsPreservesEvidenceAndAcceptsLateCosts(t *testing.T) {
	cfg := testConfig(t)
	b, err := Build(t.Context(), cfg)
	need(t, err)
	now := time.Now().UTC()
	amount := core.Money{Currency: "USD", Micros: 100}
	actual := core.Money{Currency: "USD", Micros: 30}
	need(t, b.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, id := range []core.ID{"charged", "uncertain", "not-sent", "free", "late", "missing-evidence"} {
			state := "reserved"
			if id == "late" || id == "missing-evidence" {
				state = "unknown"
			}
			row := quota.ModelRecord{OwnerID: "owner", AssistantID: "assistant", ModelID: "model", CreatedAt: now, Reservation: quota.Reservation{ID: id, OperationID: id, State: state, Reserved: amount}}
			if err := tx.Put(ctx, "security.reservations", persistence.Key("owner", id), row); err != nil {
				return err
			}
			if id == "not-sent" || id == "missing-evidence" {
				continue
			}
			r := model.Receipt{ID: id, OperationID: id, CreatedAt: now}
			if id == "charged" {
				r.OutcomeKnown = true
				r.Charged = &actual
			}
			if id == "free" {
				r.OutcomeKnown = true
				r.Failure = &model.Failure{NoCharge: true, OutcomeKnown: true}
			}
			if err := model.NewReceipts(tx.Store()).Record(ctx, "owner", r); err != nil {
				return err
			}
		}
		for _, state := range []operation.State{operation.Running, operation.Unknown, operation.Succeeded} {
			id := core.ID(state)
			op := operation.Operation{ID: id, OwnerID: "owner", AssistantID: "assistant", ExecutionID: "execution", ResourceID: "compute", IdempotencyKey: string(id), InputDigest: "digest", Action: "shell_execute", State: state, Version: 1, CreatedAt: now, UpdatedAt: now}
			if err := tx.Put(ctx, "agent.operations", persistence.Key("owner", id), op); err != nil {
				return err
			}
		}
		return nil
	}))
	need(t, b.Close())
	b, err = Build(t.Context(), cfg)
	need(t, err)
	defer b.Close()
	// Construction is also used by CLI commands and must not release live calls.
	before, err := quota.ModelTotals(t.Context(), b.store, "owner", "", time.Time{}, time.Time{})
	need(t, err)
	if before.Active != 4 {
		t.Fatalf("Build changed call holds: %+v", before)
	}
	for range 2 {
		need(t, recoverCallUsage(t.Context(), b.store))
	}
	usage, err := quota.ModelTotals(t.Context(), b.store, "owner", "", time.Time{}, time.Time{})
	need(t, err)
	if usage.Active != 0 || usage.Unknown != 3 || usage.Calls != 4 || len(usage.Reserved) != 0 || len(usage.Spent) != 1 || usage.Spent[0] != actual {
		t.Fatalf("recovered model usage: %+v", usage)
	}
	tools, err := quota.CallTotals(t.Context(), b.store, "tool", "owner", "", "", time.Time{}, time.Time{})
	need(t, err)
	if tools.Active != 0 || tools.Unknown != 2 || tools.Calls != 3 {
		t.Fatalf("recovered tools: %+v", tools)
	}
	op, err := operation.NewRepository(b.store).Get(t.Context(), "owner", "running")
	need(t, err)
	if op.State != operation.Unknown || op.Version != 2 {
		t.Fatalf("recovery replayed or reclassified effects: %+v", op)
	}
	// A real receipt arriving after local release updates spend exactly once.
	need(t, model.NewReceipts(b.store).Record(t.Context(), "owner", model.Receipt{ID: "late", OperationID: "late", CreatedAt: now, OutcomeKnown: true, Charged: &actual}))
	for range 2 {
		need(t, recoverCallUsage(t.Context(), b.store))
	}
	usage, err = quota.ModelTotals(t.Context(), b.store, "owner", "", time.Time{}, time.Time{})
	need(t, err)
	if usage.Active != 0 || usage.Unknown != 2 || len(usage.Spent) != 1 || usage.Spent[0].Micros != 60 {
		t.Fatalf("late cost duplicated or lost: %+v", usage)
	}
}
