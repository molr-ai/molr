package model

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

// The transport dependency emits "hello" followed by a provider usage receipt.
// TransportFailure injects a network timeout after dispatch. SettleProvider
// records late provider evidence; AwaitReceipt waits for real receipt persistence.
// RevokeModel/Reservation mutate actual authorization/reservation dependencies.
type contractFixture struct {
	Client            Client
	Receipts          Receipts
	Owner, Other      core.Principal
	Request           Request
	TransportCalls    func() int
	TransportFailure  func()
	SettleProvider    func(core.ID)
	AwaitReceipt      func(context.Context, core.ID) (Receipt, error)
	RevokeModel       func()
	RevokeReservation func()
}

var newContractFixture func(*testing.T) contractFixture

func TestTDDModelCatalogAndGenerationRequireCurrentAuthorization(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/model", newContractFixture)
	ctx := testkit.Context(t)
	models, err := f.Client.List(ctx, f.Owner)
	testkit.RequireNoError(t, err)
	found := false
	for _, m := range models {
		if m.ID == f.Request.ModelID {
			found = true
		}
		if m.ResourceID == "" {
			t.Fatal("model missing permission resource")
		}
	}
	if !found {
		t.Fatal("authorized model absent from catalog")
	}
	f.RevokeModel()
	_, err = f.Client.Generate(ctx, f.Request)
	testkit.RequireCode(t, err, core.Forbidden)
	if f.TransportCalls() != 0 {
		t.Fatal("unauthorized request reached provider")
	}
	models, err = f.Client.List(ctx, f.Owner)
	testkit.RequireNoError(t, err)
	for _, m := range models {
		if m.ID == f.Request.ModelID {
			t.Fatal("revoked model remains available")
		}
	}
}
func TestTDDModelReservationIsCheckedBeforeDispatch(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/model", newContractFixture)
	ctx := testkit.Context(t)
	f.RevokeReservation()
	_, err := f.Client.Generate(ctx, f.Request)
	if c := core.CodeOf(err); c != core.Forbidden && c != core.BudgetExceeded && c != core.FailedPrecondition {
		t.Fatalf("invalid reservation error = %v", err)
	}
	if f.TransportCalls() != 0 {
		t.Fatal("invalid reservation reached provider")
	}
}
func TestTDDModelStreamTerminalReceiptAndEOF(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/model", newContractFixture)
	ctx := testkit.Context(t)
	stream, err := f.Client.Generate(ctx, f.Request)
	testkit.RequireNoError(t, err)
	defer stream.Close()
	text := ""
	var terminal *Receipt
	for n := 0; n < 20; n++ {
		delta, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			if terminal == nil {
				t.Fatal("EOF before terminal result")
			}
			break
		}
		testkit.RequireNoError(t, err)
		switch delta.Kind {
		case TextDelta:
			text += delta.Text
		case Completed:
			if delta.Receipt == nil {
				t.Fatal("completion lacks receipt")
			}
			terminal = delta.Receipt
		}
	}
	if text != "hello" || terminal == nil || !terminal.OutcomeKnown || terminal.Usage.OutputTokens <= 0 {
		t.Fatalf("text = %q, terminal = %+v", text, terminal)
	}
	_, err = stream.Next(ctx)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("terminal Next = %v, want EOF", err)
	}
	receipt, err := f.Client.Receipt(ctx, f.Request.Execution, terminal.ID)
	testkit.RequireNoError(t, err)
	if receipt.OperationID != f.Request.Execution.OperationID || receipt.Usage != terminal.Usage {
		t.Fatalf("stored receipt = %+v", receipt)
	}
	foreign := f.Request.Execution
	foreign.Principal.OwnerID = f.Other.OwnerID
	_, err = f.Client.Receipt(ctx, foreign, receipt.ID)
	if c := core.CodeOf(err); c != core.NotFound && c != core.Forbidden {
		t.Fatalf("foreign receipt error = %v", err)
	}
}
func TestTDDModelClosePreservesProviderSpendAndReconciliation(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/model", newContractFixture)
	ctx := testkit.Context(t)
	stream, err := f.Client.Generate(ctx, f.Request)
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, stream.Close())
	f.SettleProvider(f.Request.Execution.OperationID)
	receipt, err := f.AwaitReceipt(ctx, f.Request.Execution.OperationID)
	testkit.RequireNoError(t, err)
	if receipt.ID == "" || !receipt.OutcomeKnown || receipt.Charged == nil || receipt.Charged.Micros <= 0 {
		t.Fatalf("closed stream lost spend: %+v", receipt)
	}
	reconciled, err := f.Client.Reconcile(ctx, f.Request.Execution, receipt.ID)
	testkit.RequireNoError(t, err)
	if reconciled.ID != receipt.ID || reconciled.Charged == nil || reconciled.Charged.Micros != receipt.Charged.Micros {
		t.Fatalf("reconciliation = %+v", reconciled)
	}
}
func TestTDDModelTransportFailureIsNotSuccessfulEOF(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/model", newContractFixture)
	ctx := testkit.Context(t)
	f.TransportFailure()
	stream, err := f.Client.Generate(ctx, f.Request)
	if err == nil {
		defer stream.Close()
		for n := 0; n < 20; n++ {
			_, err = stream.Next(ctx)
			if err != nil {
				break
			}
		}
	}
	if err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("transport failure reported success: %v", err)
	}
	receipt, err := f.AwaitReceipt(ctx, f.Request.Execution.OperationID)
	testkit.RequireNoError(t, err)
	if receipt.OutcomeKnown || (receipt.Charged != nil && receipt.Charged.Micros == 0) {
		t.Fatalf("timeout incorrectly settled as zero spend: %+v", receipt)
	}
}
func TestTDDModelReceiptsResolveUnknownWithoutErasingEvidence(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/model", newContractFixture)
	ctx := testkit.Context(t)
	original := Receipt{ID: "receipt", OperationID: f.Request.Execution.OperationID, OutcomeKnown: false, CreatedAt: time.Now()}
	testkit.RequireNoError(t, f.Receipts.Record(ctx, f.Owner.OwnerID, original))
	known := original
	known.OutcomeKnown = true
	known.ProviderRequestID = "provider-1"
	known.Usage = Usage{InputTokens: 5, OutputTokens: 3}
	known.Charged = &core.Money{Currency: "USD", Micros: 400}
	testkit.RequireNoError(t, f.Receipts.Record(ctx, f.Owner.OwnerID, known))
	testkit.RequireNoError(t, f.Receipts.Record(ctx, f.Owner.OwnerID, known))
	// A stale unknown observation may be ignored or rejected; it cannot erase spend.
	_ = f.Receipts.Record(ctx, f.Owner.OwnerID, original)
	got, err := f.Receipts.Get(ctx, f.Owner.OwnerID, known.ID)
	testkit.RequireNoError(t, err)
	byOp, err := f.Receipts.ByOperation(ctx, f.Owner.OwnerID, known.OperationID)
	testkit.RequireNoError(t, err)
	if !got.OutcomeKnown || got.Charged == nil || got.Charged.Micros != 400 || byOp.ID != got.ID {
		t.Fatalf("known receipt regressed: %+v %+v", got, byOp)
	}
	_, err = f.Receipts.Get(ctx, f.Other.OwnerID, known.ID)
	if c := core.CodeOf(err); c != core.NotFound && c != core.Forbidden {
		t.Fatalf("foreign receipt = %v", err)
	}
	forged := known
	forged.OperationID = "another-operation"
	if err = f.Receipts.Record(ctx, f.Owner.OwnerID, forged); err == nil {
		t.Fatal("existing receipt rebound to another operation")
	}
}

func TestTDDModelReceiptAndReconcileUseReceiptID(t *testing.T) {
	f := testkit.RequireFactory(t, "agent/model", newContractFixture)
	ctx := testkit.Context(t)
	original := Receipt{ID: "local-receipt-id", OperationID: f.Request.Execution.OperationID, ProviderRequestID: "provider-request-id", OutcomeKnown: true, Charged: &core.Money{Currency: "USD", Micros: 400}, CreatedAt: time.Now()}
	if original.ID == original.OperationID {
		t.Fatal("fixture must distinguish operation and receipt IDs")
	}
	testkit.RequireNoError(t, f.Receipts.Record(ctx, f.Owner.OwnerID, original))
	got, err := f.Client.Receipt(ctx, f.Request.Execution, original.ID)
	testkit.RequireNoError(t, err)
	if got.ID != original.ID || got.OperationID != original.OperationID {
		t.Fatal("receipt lookup changed identity")
	}
	reconciled, err := f.Client.Reconcile(ctx, f.Request.Execution, original.ID)
	testkit.RequireNoError(t, err)
	if reconciled.ID != original.ID || reconciled.OperationID != original.OperationID {
		t.Fatal("reconciliation changed identity")
	}
	for _, wrongID := range []core.ID{original.OperationID, core.ID(original.ProviderRequestID)} {
		_, err = f.Client.Receipt(ctx, f.Request.Execution, wrongID)
		testkit.RequireCode(t, err, core.NotFound)
		_, err = f.Client.Reconcile(ctx, f.Request.Execution, wrongID)
		testkit.RequireCode(t, err, core.NotFound)
	}
	if f.TransportCalls() != 0 {
		t.Fatal("receipt reconciliation generated another model request")
	}
}
