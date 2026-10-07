package policy

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

// Fixture seeds a live owner, assistant, resource and USD 100-micro budget in
// real persistence. Execution creates persisted operation/execution records.
type contractFixture struct {
	Service         Service
	Repository      Repository
	Owner           core.Principal
	OtherOwner      core.Principal
	Assistant       core.Principal
	ResourceID      core.ID
	Now             time.Time
	Execution       func(core.ID) core.Execution
	Advance         func(time.Duration)
	RevokeActor     func(core.Principal)
	ConfirmNoCharge func(core.ID)
	FailStorage     func()
}

var newContractFixture func(*testing.T) contractFixture

func fixture(t *testing.T) contractFixture {
	t.Helper()
	return testkit.RequireFactory(t, "policy", newContractFixture)
}
func money(n int64) core.Money { return core.Money{Currency: "USD", Micros: n} }
func grant(t *testing.T, f contractFixture) Grant {
	t.Helper()
	g, err := f.Service.Grant(testkit.Context(t), f.Owner, GrantRequest{Mutation: core.Mutation{IdempotencyKey: "grant"}, AssistantID: f.Assistant.SubjectID, ResourceID: f.ResourceID, Actions: []Action{Read}, Scope: Scope{PathPrefixes: []string{"/documents"}, ReadOnly: true}})
	testkit.RequireNoError(t, err)
	return g
}
func TestTDDGrantsAndLiveAuthorization(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	check := CheckRequest{Target: core.Object{Kind: "resource", ID: f.ResourceID}, ResourceID: f.ResourceID, Action: Read, Destination: "/documents/report.txt"}
	if err := f.Service.Require(ctx, f.Assistant, check); err == nil {
		t.Fatal("assistant authorized without grant")
	}
	g := grant(t, f)
	testkit.RequireNoError(t, f.Service.Require(ctx, f.Assistant, check))
	p, err := f.Service.List(ctx, f.Owner, Query{AssistantID: f.Assistant.SubjectID, ResourceID: f.ResourceID})
	testkit.RequireNoError(t, err)
	if len(p.Items) != 1 || p.Items[0].ID != g.ID {
		t.Fatal("grant not listed")
	}
	for _, modify := range []func(*CheckRequest){func(r *CheckRequest) { r.Action = Write }, func(r *CheckRequest) { r.Destination = "/private/report.txt" }, func(r *CheckRequest) { r.ResourceID = "different-resource" }} {
		bad := check
		modify(&bad)
		if err := f.Service.Require(ctx, f.Assistant, bad); err == nil {
			t.Fatal("grant scope bypassed")
		}
	}
	testkit.RequireNoError(t, f.Service.Revoke(ctx, f.Owner, RevokeRequest{Mutation: core.Mutation{IdempotencyKey: "revoke", ExpectedVersion: g.Version}, GrantID: g.ID}))
	if err := f.Service.Require(ctx, f.Assistant, check); err == nil {
		t.Fatal("grant revocation not live")
	}
}
func TestTDDPolicyFailsClosedAndOwnerManagement(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	g := grant(t, f)
	for _, p := range []core.Principal{f.Assistant, f.OtherOwner, {}} {
		if err := f.Service.Revoke(ctx, p, RevokeRequest{Mutation: core.Mutation{IdempotencyKey: "forbidden-revoke", ExpectedVersion: g.Version}, GrantID: g.ID}); err == nil {
			t.Fatal("unauthorized grant revoke")
		}
	}
	f.RevokeActor(f.Owner)
	if err := f.Service.Require(ctx, f.Owner, CheckRequest{Target: core.Object{Kind: "resource", ID: f.ResourceID}, Action: Read}); err == nil {
		t.Fatal("revoked principal authorized")
	}
	f.FailStorage()
	if err := f.Service.Require(ctx, f.Assistant, CheckRequest{Target: core.Object{Kind: "resource", ID: f.ResourceID}, ResourceID: f.ResourceID, Action: Read}); err == nil {
		t.Fatal("policy storage outage failed open")
	}
}

func TestResourceCapabilitiesFailClosed(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(fmt.Sprint(unavailable), func(t *testing.T) {
			f := fixture(t)
			grant(t, f)
			manager := f.Service.(*Manager)
			manager.options.ResourceActions = nil
			if unavailable {
				manager.options.ResourceActions = func(context.Context, core.Principal, core.ID) ([]string, error) {
					return nil, core.NewError(core.Unavailable, "capability resolver unavailable")
				}
			}
			_, err := f.Service.Grant(t.Context(), f.Owner, GrantRequest{Mutation: core.Mutation{IdempotencyKey: "new-grant"}, AssistantID: f.Assistant.SubjectID, ResourceID: f.ResourceID, Actions: []Action{Read}})
			testkit.RequireCode(t, err, core.Unavailable)
			err = f.Service.Require(t.Context(), f.Assistant, CheckRequest{Target: core.Object{Kind: "resource", ID: f.ResourceID}, ResourceID: f.ResourceID, Action: Read, Destination: "/documents/report.txt"})
			testkit.RequireCode(t, err, core.Unavailable)
		})
	}
}
func TestTDDBudgetAtomicReservationAndIdempotency(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	var wg sync.WaitGroup
	type result struct {
		r   Reservation
		err error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		e := f.Execution(core.ID(fmt.Sprintf("operation-%d", i)))
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := f.Service.Reserve(ctx, ReserveRequest{Execution: e, Estimate: money(60)})
			results <- result{r, err}
		}()
	}
	wg.Wait()
	close(results)
	wins := 0
	for result := range results {
		if result.err == nil {
			wins++
		} else {
			testkit.RequireCode(t, result.err, core.BudgetExceeded)
		}
	}
	if wins != 1 {
		t.Fatalf("concurrent reservations exceeding budget: winners=%d", wins)
	}
	usage, err := f.Service.Usage(ctx, f.Owner, UsageQuery{})
	testkit.RequireNoError(t, err)
	if usage.Reserved != money(60) || usage.Spent != money(0) {
		t.Fatal("non-atomic budget totals")
	}
	e := f.Execution("idempotent-operation")
	r, err := f.Service.Reserve(ctx, ReserveRequest{Execution: e, Estimate: money(30)})
	testkit.RequireNoError(t, err)
	again, err := f.Service.Reserve(ctx, ReserveRequest{Execution: e, Estimate: money(30)})
	testkit.RequireNoError(t, err)
	if again.ID != r.ID {
		t.Fatal("retry created another reservation")
	}
	_, err = f.Service.Reserve(ctx, ReserveRequest{Execution: e, Estimate: money(20)})
	testkit.RequireCode(t, err, core.Conflict)
	_, err = f.Service.Reserve(ctx, ReserveRequest{Execution: f.Execution("foreign-currency"), Estimate: core.Money{Currency: "EUR", Micros: 1}})
	if err == nil {
		t.Fatal("currency mismatch accepted")
	}
}
func TestTDDBudgetSettlementUnknownAndRelease(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	e := f.Execution("charged")
	r, err := f.Service.Reserve(ctx, ReserveRequest{Execution: e, Estimate: money(60)})
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, f.Service.MarkUnknown(ctx, e, r.ID))
	u, err := f.Service.Usage(ctx, f.Owner, UsageQuery{})
	testkit.RequireNoError(t, err)
	if u.Reserved != money(0) || len(u.UnknownOperations) != 1 || u.UnknownOperations[0] != e.OperationID {
		t.Fatal("unknown operation kept a hold or lost its history")
	}
	if err := f.Service.Release(ctx, e, r.ID); err == nil {
		t.Fatal("unknown charge released without evidence")
	}
	request := SettleRequest{ReservationID: r.ID, OperationID: e.OperationID, ReceiptID: "receipt", Actual: money(45)}
	settled, err := f.Service.Settle(ctx, e, request)
	testkit.RequireNoError(t, err)
	if settled.Settled == nil || *settled.Settled != money(45) {
		t.Fatal("settlement incorrect")
	}
	_, err = f.Service.Settle(ctx, e, request)
	testkit.RequireNoError(t, err)
	u, err = f.Service.Usage(ctx, f.Owner, UsageQuery{})
	testkit.RequireNoError(t, err)
	if u.Spent != money(45) || u.Reserved != money(0) || len(u.UnknownOperations) != 0 {
		t.Fatal("duplicate settlement or lingering reservation")
	}
	free := f.Execution("no-charge")
	reserved, err := f.Service.Reserve(ctx, ReserveRequest{Execution: free, Estimate: money(30)})
	testkit.RequireNoError(t, err)
	f.ConfirmNoCharge(free.OperationID)
	testkit.RequireNoError(t, f.Service.Release(ctx, free, reserved.ID))
	u, err = f.Service.Usage(ctx, f.Owner, UsageQuery{})
	testkit.RequireNoError(t, err)
	if u.Reserved != money(0) || u.Spent != money(45) {
		t.Fatal("release changed paid amount")
	}
}
func TestTDDRepositoryGrantIsolationAndCAS(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	r := f.Repository
	g := Grant{ID: "grant", OwnerID: f.Owner.OwnerID, AssistantID: f.Assistant.SubjectID, ResourceID: f.ResourceID, Actions: []Action{Read}, Version: 1}
	testkit.RequireNoError(t, r.InsertGrant(ctx, g))
	p, err := r.Grants(ctx, f.Owner.OwnerID, Query{})
	testkit.RequireNoError(t, err)
	if len(p.Items) != 1 || p.Items[0].ID != g.ID {
		t.Fatal("inserted grant missing")
	}
	p, err = r.Grants(ctx, f.OtherOwner.OwnerID, Query{})
	testkit.RequireNoError(t, err)
	if len(p.Items) != 0 {
		t.Fatal("cross-owner grants")
	}
	if err = r.RevokeGrant(ctx, f.OtherOwner.OwnerID, g.ID, g.Version, f.Now); err == nil {
		t.Fatal("cross-owner revoke succeeded")
	}
	testkit.RequireNoError(t, r.RevokeGrant(ctx, f.Owner.OwnerID, g.ID, g.Version, f.Now))
	if err = r.RevokeGrant(ctx, f.Owner.OwnerID, g.ID, g.Version, f.Now); err == nil {
		t.Fatal("stale grant version accepted")
	}
	p, err = r.Grants(ctx, f.Owner.OwnerID, Query{})
	testkit.RequireNoError(t, err)
	for _, item := range p.Items {
		if item.ID == g.ID && item.RevokedAt == nil {
			t.Fatal("revoked grant remains active")
		}
	}
}
func TestTDDRepositoryBudgetLifecycle(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	r := f.Repository
	e := f.Execution("repository-operation")
	reserved, err := r.Reserve(ctx, ReserveRequest{Execution: e, Estimate: money(70)})
	testkit.RequireNoError(t, err)
	again, err := r.Reserve(ctx, ReserveRequest{Execution: e, Estimate: money(70)})
	testkit.RequireNoError(t, err)
	if again.ID != reserved.ID {
		t.Fatal("repository retry duplicated reservation")
	}
	_, err = r.Reserve(ctx, ReserveRequest{Execution: f.Execution("over-limit"), Estimate: money(40)})
	testkit.RequireCode(t, err, core.BudgetExceeded)
	if err = r.SetReservationState(ctx, f.OtherOwner.OwnerID, reserved.ID, "unknown"); err == nil {
		t.Fatal("foreign owner modified reservation")
	}
	testkit.RequireNoError(t, r.SetReservationState(ctx, f.Owner.OwnerID, reserved.ID, "unknown"))
	u, err := r.Usage(ctx, f.Owner.OwnerID, UsageQuery{})
	testkit.RequireNoError(t, err)
	if u.Reserved != money(0) || len(u.UnknownOperations) != 1 {
		t.Fatal("unknown cost kept a hold or lost its history")
	}
	settled, err := r.Settle(ctx, f.Owner.OwnerID, SettleRequest{ReservationID: reserved.ID, OperationID: e.OperationID, ReceiptID: "receipt", Actual: money(55)})
	testkit.RequireNoError(t, err)
	if settled.Settled == nil || *settled.Settled != money(55) {
		t.Fatal("repository settlement incorrect")
	}
	u, err = r.Usage(ctx, f.Owner.OwnerID, UsageQuery{})
	testkit.RequireNoError(t, err)
	if u.Spent != money(55) || u.Reserved != money(0) {
		t.Fatal("repository usage incorrect")
	}
}

func TestGrantUpdateIsAtomicVersionedAndOwnerOnly(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	g := grant(t, f)
	req := UpdateGrantRequest{Mutation: core.Mutation{IdempotencyKey: "edit", ExpectedVersion: g.Version}, GrantID: g.ID, Actions: []Action{Read}, Scope: Scope{ReadOnly: true, PathPrefixes: []string{"/reports"}}}
	if _, err := f.Service.UpdateGrant(ctx, f.OtherOwner, req); err == nil {
		t.Fatal("foreign owner edited grant")
	}
	if _, err := f.Service.UpdateGrant(ctx, f.Assistant, req); err == nil {
		t.Fatal("assistant edited grant")
	}
	updated, err := f.Service.UpdateGrant(ctx, f.Owner, req)
	testkit.RequireNoError(t, err)
	if updated.ID != g.ID || updated.AssistantID != g.AssistantID || updated.ResourceID != g.ResourceID || updated.Version != g.Version+1 || len(updated.Scope.PathPrefixes) != 1 || updated.Scope.PathPrefixes[0] != "/reports" {
		t.Fatalf("grant update=%+v", updated)
	}
	repeated, err := f.Service.UpdateGrant(ctx, f.Owner, req)
	testkit.RequireNoError(t, err)
	if repeated.Version != updated.Version {
		t.Fatal("retry edited grant twice")
	}
	req.Mutation.IdempotencyKey = "stale"
	_, err = f.Service.UpdateGrant(ctx, f.Owner, req)
	testkit.RequireCode(t, err, core.Conflict)
	req.Mutation.ExpectedVersion = updated.Version
	req.Scope.PathPrefixes = []string{"../escape"}
	_, err = f.Service.UpdateGrant(ctx, f.Owner, req)
	testkit.RequireCode(t, err, core.InvalidArgument)
	page, err := f.Service.List(ctx, f.Owner, Query{})
	testkit.RequireNoError(t, err)
	if len(page.Items) != 1 || page.Items[0].Version != updated.Version || page.Items[0].Scope.PathPrefixes[0] != "/reports" {
		t.Fatal("invalid edit changed stored grant")
	}
	testkit.RequireNoError(t, f.Service.Revoke(ctx, f.Owner, RevokeRequest{Mutation: core.Mutation{IdempotencyKey: "revoke-edit", ExpectedVersion: updated.Version}, GrantID: g.ID}))
	req.Mutation.ExpectedVersion++
	req.Scope.PathPrefixes = []string{"/reports"}
	_, err = f.Service.UpdateGrant(ctx, f.Owner, req)
	testkit.RequireCode(t, err, core.FailedPrecondition)
}
