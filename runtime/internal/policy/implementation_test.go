package policy

import (
	"context"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fixtureActor struct {
	OwnerID core.ID
	ID      core.ID
	State   string
	Budget  core.AssistantBudget
}

func init() {
	newContractFixture = func(t *testing.T) contractFixture {
		store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "policy.db"))
		testkit.RequireNoError(t, err)
		t.Cleanup(func() { _ = store.Close() })
		ctx := testkit.Context(t)
		now := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
		initial := now
		var mu sync.Mutex
		clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
		validateAssistant := func(ctx context.Context, p core.Principal) error {
			a, e := persistence.Load[fixtureActor](ctx, store, "business.assistants", persistence.Key(p.OwnerID, p.SubjectID))
			if e != nil {
				return e
			}
			if a.State != "active" {
				return core.NewError(core.Forbidden, "assistant inactive")
			}
			var execution core.Principal
			e = store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
				return tx.Get(ctx, "security.test_executions", persistence.Key(p.OwnerID, p.ExecutionID), &execution)
			})
			if e != nil {
				return e
			}
			if execution != p {
				return core.NewError(core.Forbidden, "execution identity mismatch")
			}
			return nil
		}
		authentication, err := auth.NewService(store, auth.Options{MasterKey: make([]byte, 32), Origin: "https://molr.example", Now: clock, ValidateAssistant: validateAssistant})
		testkit.RequireNoError(t, err)
		account, err := authentication.Initialize(ctx, auth.InitializeRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!")})
		testkit.RequireNoError(t, err)
		login, err := authentication.Login(ctx, auth.LoginRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!")})
		testkit.RequireNoError(t, err)
		owner, err := authentication.Authenticate(ctx, auth.Authentication{Session: login.Session.Token})
		testkit.RequireNoError(t, err)
		assistant := core.Principal{OwnerID: account.ID, Kind: core.ActorAssistant, SubjectID: "assistant", ExecutionID: "execution"}
		testkit.RequireNoError(t, store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			if e := tx.Put(ctx, "business.assistants", persistence.Key(account.ID, "assistant"), fixtureActor{account.ID, "assistant", "active", core.AssistantBudget{Limit: money(100), Period: "daily"}}); e != nil {
				return e
			}
			if e := tx.Put(ctx, "business.resources", persistence.Key(account.ID, "resource"), fixtureActor{account.ID, "resource", "active", core.AssistantBudget{}}); e != nil {
				return e
			}
			if e := tx.Put(ctx, "security.test_executions", persistence.Key(account.ID, "execution"), assistant); e != nil {
				return e
			}
			return nil
		}))
		owns := func(ctx context.Context, p core.Principal, o core.Object) error {
			bucket := ""
			switch o.Kind {
			case "assistant":
				bucket = "business.assistants"
			case "resource":
				bucket = "business.resources"
			default:
				return core.NewError(core.Forbidden, "unknown target")
			}
			v, e := persistence.Load[fixtureActor](ctx, store, bucket, persistence.Key(p.OwnerID, o.ID))
			if e != nil {
				return e
			}
			if v.OwnerID != p.OwnerID {
				return core.NewError(core.Forbidden, "foreign target")
			}
			return nil
		}
		validateExecution := func(ctx context.Context, e core.Execution) error {
			saved, err := persistence.Load[core.Execution](ctx, store, "security.test_operations", persistence.Key(e.Principal.OwnerID, e.OperationID))
			if err != nil {
				return err
			}
			if saved != e {
				return core.NewError(core.Forbidden, "operation mismatch")
			}
			return nil
		}
		verifyReceipt := func(ctx context.Context, e core.Execution, id core.ID, actual core.Money) error {
			receipt, err := persistence.Load[struct {
				OperationID core.ID
				Actual      core.Money
			}](ctx, store, "security.test_receipts", persistence.Key(e.Principal.OwnerID, id))
			if err != nil {
				return err
			}
			if receipt.OperationID != e.OperationID || receipt.Actual != actual {
				return core.NewError(core.Forbidden, "receipt mismatch")
			}
			return nil
		}
		noCharge := func(ctx context.Context, e core.Execution) (bool, error) {
			v, err := persistence.Load[bool](ctx, store, "security.test_nocharge", persistence.Key(e.Principal.OwnerID, e.OperationID))
			if persistence.Missing(err) {
				return false, nil
			}
			return v, err
		}
		service, err := NewService(store, Options{Now: clock, ValidatePrincipal: authentication.ValidatePrincipal, CheckScope: authentication.CheckScope, Owns: owns, ResourceActions: func(context.Context, core.Principal, core.ID) ([]string, error) { return []string{"read"}, nil }, ValidateExecution: validateExecution, VerifyReceipt: verifyReceipt, NoCharge: noCharge})
		testkit.RequireNoError(t, err)
		return contractFixture{Service: service, Repository: NewRepository(store), Owner: owner, OtherOwner: core.Principal{OwnerID: "other", SubjectID: "other", Kind: core.ActorOwner, SessionID: "other-session"}, Assistant: assistant, ResourceID: "resource", Now: initial, Execution: func(id core.ID) core.Execution {
			e := core.Execution{Principal: assistant, AssistantID: assistant.SubjectID, OperationID: id}
			testkit.RequireNoError(t, store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				if err := tx.Put(ctx, "security.test_operations", persistence.Key(account.ID, id), e); err != nil {
					return err
				}
				return tx.Put(ctx, "security.test_receipts", persistence.Key(account.ID, "receipt"), struct {
					OperationID core.ID
					Actual      core.Money
				}{id, money(45)})
			}))
			return e
		}, Advance: func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }, RevokeActor: func(p core.Principal) {
			testkit.RequireNoError(t, authentication.RevokeSession(ctx, owner, p.SessionID))
		}, ConfirmNoCharge: func(id core.ID) {
			testkit.RequireNoError(t, store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				return tx.Put(ctx, "security.test_nocharge", persistence.Key(account.ID, id), true)
			}))
		}, FailStorage: func() { testkit.RequireNoError(t, store.Close()) }}
	}
}

func TestAssistantBudgetsAreIndependent(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	manager := f.Service.(*Manager)
	first := f.Execution("first-operation")
	_, err := manager.Reserve(ctx, ReserveRequest{Execution: first, Estimate: money(60)})
	testkit.RequireNoError(t, err)
	second := first
	second.AssistantID = "second-assistant"
	second.OperationID = "second-operation"
	second.Principal.SubjectID = second.AssistantID
	second.Principal.ExecutionID = "second-execution"
	testkit.RequireNoError(t, manager.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if e := tx.Put(ctx, "business.assistants", persistence.Key(f.Owner.OwnerID, second.AssistantID), fixtureActor{f.Owner.OwnerID, second.AssistantID, "active", core.AssistantBudget{Limit: money(100), Period: "daily"}}); e != nil {
			return e
		}
		if e := tx.Put(ctx, "security.test_executions", persistence.Key(f.Owner.OwnerID, second.Principal.ExecutionID), second.Principal); e != nil {
			return e
		}
		return tx.Put(ctx, "security.test_operations", persistence.Key(f.Owner.OwnerID, second.OperationID), second)
	}))
	_, err = manager.Reserve(ctx, ReserveRequest{Execution: second, Estimate: money(60)})
	testkit.RequireNoError(t, err)
}
func TestAssistantBudgetMustBeExplicit(t *testing.T) {
	f := fixture(t)
	m := f.Service.(*Manager)
	ctx := testkit.Context(t)
	testkit.RequireNoError(t, m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "business.assistants", persistence.Key(f.Owner.OwnerID, "assistant"), fixtureActor{f.Owner.OwnerID, "assistant", "active", core.AssistantBudget{}})
	}))
	_, err := m.Reserve(ctx, ReserveRequest{Execution: f.Execution("missing-budget"), Estimate: money(0)})
	testkit.RequireCode(t, err, core.BudgetExceeded)
	if core.ReasonOf(err) != core.AssistantBudgetMissing {
		t.Fatal("missing specific budget reason")
	}
}

func TestHistoricalSettlementDoesNotAuthorizeNewExecution(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	m := f.Service.(*Manager)
	e := f.Execution("charged-operation")
	r, err := m.Reserve(ctx, ReserveRequest{Execution: e, Estimate: money(60)})
	testkit.RequireNoError(t, err)
	m.options.ValidateSettlement = func(ctx context.Context, e core.Execution) error {
		stored, err := persistence.Load[core.Execution](ctx, m.store, "security.test_operations", persistence.Key(e.Principal.OwnerID, e.OperationID))
		if err != nil {
			return err
		}
		if stored != e {
			return core.NewError(core.Forbidden, "historical execution mismatch")
		}
		return nil
	}
	m.options.ValidateExecution = func(context.Context, core.Execution) error { return core.NewError(core.Forbidden, "execution stopped") }
	testkit.RequireNoError(t, m.MarkUnknown(ctx, e, r.ID))
	settled, err := m.Settle(ctx, e, SettleRequest{ReservationID: r.ID, OperationID: e.OperationID, ReceiptID: "receipt", Actual: money(45)})
	testkit.RequireNoError(t, err)
	if settled.Settled == nil || settled.Settled.Micros != 45 {
		t.Fatal("late charge lost after cancellation")
	}
	_, err = m.Reserve(ctx, ReserveRequest{Execution: e, Estimate: money(60)})
	testkit.RequireCode(t, err, core.Forbidden)
	forged := e
	forged.Principal.ExecutionID = "forged-execution"
	if err = m.MarkUnknown(ctx, forged, r.ID); err == nil {
		t.Fatal("fabricated historical execution accepted")
	}
}
