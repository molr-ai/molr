package agent

import (
	"context"
	"testing"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/tests/testkit"
)

func TestUncheckpointedModelResultUsesFreshAttemptAfterRestart(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		name := "completed"
		if unknown {
			name = "unknown"
		}
		t.Run(name, func(t *testing.T) {
			f, db := makeLoopFixture(t, nil)
			e := f.Runtime.(*Engine)
			p := turnProgress{Inputs: []model.Input{{Role: model.System, Text: loopInstructions}, {Role: model.User, Text: "resume this request"}}}
			logical := modelOperationID(f.Turn, 0)
			_, interrupted, err := e.generateContextStep(t.Context(), f.Turn, "model", nil, p, logical, 100)
			testkit.RequireNoError(t, err)
			if interrupted {
				t.Fatal("unexpected interrupt")
			}
			before, err := model.NewReceipts(db).ByOperation(t.Context(), f.Owner.OwnerID, logical)
			testkit.RequireNoError(t, err)
			if unknown {
				before.OutcomeKnown, before.Charged = false, nil
				testkit.RequireNoError(t, db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
					return tx.Put(ctx, "agent.model_receipts", persistence.Key(f.Owner.OwnerID, before.ID), before)
				}))
			}
			// The process exited before committing the returned message to a checkpoint.
			restarted := f.Restart().(*Engine)
			result, interrupted, err := restarted.generateContextStep(t.Context(), f.Turn, "model", nil, p, logical, 100)
			testkit.RequireNoError(t, err)
			requests := f.ModelRequests()
			if interrupted || result.Text != "private completion" || len(requests) != 2 || requests[0].Execution.OperationID == requests[1].Execution.OperationID || requests[0].ReservationID == requests[1].ReservationID {
				t.Fatal("replacement reused operation or reservation", requests)
			}
			receipts, err := persistence.Scan[model.Receipt](t.Context(), db, "agent.model_receipts")
			testkit.RequireNoError(t, err)
			if len(receipts) != 2 {
				t.Fatal("original billing record overwritten", receipts)
			}
			after, err := model.NewReceipts(db).ByOperation(t.Context(), f.Owner.OwnerID, logical)
			testkit.RequireNoError(t, err)
			if contextHash(before) != contextHash(after) {
				t.Fatal("replacement changed previous receipt")
			}
			testkit.RequireNoError(t, db.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
				rows, err := tx.Scan(ctx, "agent.model_requests")
				if len(rows) != 0 {
					t.Fatal("legacy result cache still written")
				}
				return err
			}))
		})
	}
}
