package operation

import (
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

func TestTDDOperationTerminalRecordCannotBeRewritten(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/operation", newContractFixture)
	ctx := testkit.Context(t)
	op, err := f.Service.Begin(ctx, f.Begin)
	testkit.RequireNoError(t, err)
	done, err := f.Service.Complete(ctx, f.Begin.Execution, Completion{ID: op.ID, ExpectedVersion: op.Version, State: Succeeded})
	testkit.RequireNoError(t, err)
	rewrite := done
	rewrite.State = Unknown
	rewrite.Version++
	testkit.RequireCode(t, f.Repository.Save(ctx, rewrite, done.Version), core.FailedPrecondition)
	persisted, err := f.Repository.Get(ctx, done.OwnerID, done.ID)
	testkit.RequireNoError(t, err)
	if persisted.State != Succeeded || persisted.Version != done.Version {
		t.Fatal("terminal operation was overwritten")
	}
}
