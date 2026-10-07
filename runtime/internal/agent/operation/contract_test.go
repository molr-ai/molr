package operation

import (
	"encoding/json"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

// ProviderSettled installs evidence on the external reconciliation dependency.
// The Repository and Service share a clean database containing an active execution.
type contractFixture struct {
	Service         Service
	Repository      Repository
	Owner, Other    core.Principal
	Begin           BeginRequest
	ProviderSettled func(core.ID)
	ExternalCalls   func() int
}

var newContractFixture func(*testing.T) contractFixture

func TestTDDOperationBeginDeduplicatesIntentAndRejectsPayloadChange(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/operation", newContractFixture)
	ctx := testkit.Context(t)
	first, err := f.Service.Begin(ctx, f.Begin)
	testkit.RequireNoError(t, err)
	again, err := f.Service.Begin(ctx, f.Begin)
	testkit.RequireNoError(t, err)
	if first.ID == "" || first.ID != again.ID || first.OwnerID != f.Owner.OwnerID || first.ExecutionID != f.Begin.Execution.Principal.ExecutionID {
		t.Fatalf("intent binding = %+v %+v", first, again)
	}
	stored, err := f.Service.Get(ctx, f.Owner, first.ID)
	testkit.RequireNoError(t, err)
	if stored.InputDigest != f.Begin.InputDigest {
		t.Fatal("intent digest not persisted")
	}
	changed := f.Begin
	changed.InputDigest = "changed"
	_, err = f.Service.Begin(ctx, changed)
	testkit.RequireCode(t, err, core.Conflict)
	page, err := f.Service.List(ctx, f.Owner, Query{AssistantID: f.Begin.Execution.AssistantID})
	testkit.RequireNoError(t, err)
	if len(page.Items) != 1 || page.Items[0].ID != first.ID {
		t.Fatalf("operations = %+v", page)
	}
}
func TestTDDOperationCompletionChecksIdentityAndVersion(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/operation", newContractFixture)
	ctx := testkit.Context(t)
	op, err := f.Service.Begin(ctx, f.Begin)
	testkit.RequireNoError(t, err)
	done := Completion{ID: op.ID, ExpectedVersion: op.Version, State: Succeeded, SafeResult: json.RawMessage(`{"ok":true}`), ReceiptID: "receipt"}
	foreign := f.Begin.Execution
	foreign.Principal.OwnerID = f.Other.OwnerID
	_, err = f.Service.Complete(ctx, foreign, done)
	if c := core.CodeOf(err); c != core.Forbidden && c != core.NotFound {
		t.Fatalf("foreign completion = %v", err)
	}
	completed, err := f.Service.Complete(ctx, f.Begin.Execution, done)
	testkit.RequireNoError(t, err)
	if completed.State != Succeeded || completed.Version <= op.Version || completed.ReceiptID != "receipt" {
		t.Fatalf("completed = %+v", completed)
	}
	done.State = Failed
	_, err = f.Service.Complete(ctx, f.Begin.Execution, done)
	testkit.RequireCode(t, err, core.Conflict)
	got, err := f.Service.Get(ctx, f.Owner, op.ID)
	testkit.RequireNoError(t, err)
	if got.State != Succeeded {
		t.Fatal("stale callback replaced successful result")
	}
	_, err = f.Service.Get(ctx, f.Other, op.ID)
	if c := core.CodeOf(err); c != core.Forbidden && c != core.NotFound {
		t.Fatalf("foreign get = %v", err)
	}
}
func TestTDDOperationUnknownRequiresReconciliationWithoutBlindRetry(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/operation", newContractFixture)
	ctx := testkit.Context(t)
	op, err := f.Service.Begin(ctx, f.Begin)
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, f.Service.MarkUnknown(ctx, f.Begin.Execution, op.ID))
	unknown, err := f.Service.Get(ctx, f.Owner, op.ID)
	testkit.RequireNoError(t, err)
	if unknown.State != Unknown {
		t.Fatalf("state = %s", unknown.State)
	}
	before := f.ExternalCalls()
	f.ProviderSettled(op.ID)
	reconciled, err := f.Service.Reconcile(ctx, f.Owner, ReconcileRequest{ID: op.ID, ExpectedVersion: unknown.Version})
	testkit.RequireNoError(t, err)
	if reconciled.State != Succeeded || reconciled.ReceiptID == "" || f.ExternalCalls() != before {
		t.Fatalf("reconciliation repeated external effect or lost receipt: %+v", reconciled)
	}
}
func TestTDDOperationCancellationAndRepositoryIsolation(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/operation", newContractFixture)
	ctx := testkit.Context(t)
	op, err := f.Service.Begin(ctx, f.Begin)
	testkit.RequireNoError(t, err)
	_, err = f.Service.Cancel(ctx, f.Other, op.ID)
	if c := core.CodeOf(err); c != core.Forbidden && c != core.NotFound {
		t.Fatalf("foreign cancel = %v", err)
	}
	canceled, err := f.Service.Cancel(ctx, f.Owner, op.ID)
	testkit.RequireNoError(t, err)
	if canceled.State != Canceled {
		t.Fatalf("cancel = %+v", canceled)
	}
	stored, err := f.Repository.Get(ctx, f.Owner.OwnerID, op.ID)
	testkit.RequireNoError(t, err)
	if stored.State != Canceled {
		t.Fatal("cancellation not durable")
	}
	byKey, err := f.Repository.FindByKey(ctx, f.Owner.OwnerID, f.Begin.Mutation.IdempotencyKey)
	testkit.RequireNoError(t, err)
	if byKey.ID != op.ID {
		t.Fatal("idempotency lookup mismatch")
	}
	page, err := f.Repository.List(ctx, f.Owner.OwnerID, Query{States: []State{Canceled}})
	testkit.RequireNoError(t, err)
	if len(page.Items) != 1 || page.Items[0].ID != op.ID {
		t.Fatalf("filtered page = %+v", page)
	}
	_, err = f.Repository.Get(ctx, f.Other.OwnerID, op.ID)
	if c := core.CodeOf(err); c != core.Forbidden && c != core.NotFound {
		t.Fatalf("repository foreign get = %v", err)
	}
}
func TestTDDOperationRepositoryRejectsDuplicateAndStaleSave(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/operation", newContractFixture)
	ctx := testkit.Context(t)
	op := Operation{ID: "stored-op", OwnerID: f.Owner.OwnerID, AssistantID: f.Begin.Execution.AssistantID, ExecutionID: f.Begin.Execution.Principal.ExecutionID, ResourceID: f.Begin.ResourceID, Action: f.Begin.Action, State: Prepared, IdempotencyKey: "repo-key", InputDigest: "digest", Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	testkit.RequireNoError(t, f.Repository.Insert(ctx, op))
	if err := f.Repository.Insert(ctx, op); err == nil {
		t.Fatal("duplicate operation accepted")
	}
	op.State = Running
	op.Version = 2
	testkit.RequireNoError(t, f.Repository.Save(ctx, op, 1))
	op.State = Failed
	op.Version = 3
	testkit.RequireCode(t, f.Repository.Save(ctx, op, 1), core.Conflict)
	got, err := f.Repository.Get(ctx, f.Owner.OwnerID, op.ID)
	testkit.RequireNoError(t, err)
	if got.State != Running || got.Version != 2 {
		t.Fatalf("stale update persisted: %+v", got)
	}
}
