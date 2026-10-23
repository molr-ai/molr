package resource

import (
	"context"
	"errors"
	"testing"

	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
)

type receiptAuthorizer struct {
	base   policy.Authorizer
	denied bool
}

func (a *receiptAuthorizer) Require(ctx context.Context, p core.Principal, q policy.CheckRequest) error {
	if a.denied {
		return core.NewError(core.Forbidden, "access revoked")
	}
	return a.base.Require(ctx, p, q)
}

func TestResourceReceiptRechecksAuthorizationAndKeepsAuditAtomic(t *testing.T) {
	f := newContractFixture(t)
	s := f.Service.(*service)
	gate := &receiptAuthorizer{base: s.options.Authorizer}
	s.options.Authorizer = gate
	ctx := t.Context()
	request := ConnectRequest{Mutation: mutation("receipt-check", 0), Kind: Storage, Name: "Documents", Provider: f.SupportedProvider, Binding: f.SafeBinding}
	first, err := f.Service.Connect(ctx, owner(), request)
	if err != nil {
		t.Fatal(err)
	}
	auditCount := func() int {
		t.Helper()
		page, err := audit.NewRepository(s.store).List(ctx, owner().OwnerID, audit.Query{Action: "resource.connect"})
		if err != nil {
			t.Fatal(err)
		}
		return len(page.Items)
	}
	if auditCount() != 1 {
		t.Fatal("initial mutation was not audited exactly once")
	}
	gate.denied = true
	if _, err := f.Service.Connect(ctx, owner(), request); core.CodeOf(err) != core.Forbidden {
		t.Fatal("receipt bypassed revocation", err)
	}
	gate.denied = false
	replay, err := f.Service.Connect(ctx, owner(), request)
	if err != nil || replay.ID != first.ID || auditCount() != 1 {
		t.Fatal("replay repeated business/audit writes", replay, err)
	}
	request.Name = "different payload"
	if _, err := f.Service.Connect(ctx, owner(), request); core.CodeOf(err) != core.Conflict {
		t.Fatal("digest mismatch accepted", err)
	}
	request.Mutation = mutation("retry-after-rollback", 0)
	s.store.(*sqlitestore.Store).FailNextCommit(errors.New("commit interrupted"))
	if _, err := f.Service.Connect(ctx, owner(), request); err == nil {
		t.Fatal("commit failure lost")
	}
	page, err := f.Repository.List(ctx, owner().OwnerID, Query{Name: request.Name})
	if err != nil || len(page.Items) != 0 || auditCount() != 1 {
		t.Fatal("partial resource/audit committed", page, err)
	}
	key := persistence.Key(owner().OwnerID, core.ID("connect//"+request.Mutation.IdempotencyKey))
	if _, err := persistence.Load[persistence.Receipt](ctx, s.store, receiptBucket, key); !persistence.Missing(err) {
		t.Fatal("receipt survived rollback", err)
	}
	created, err := f.Service.Connect(ctx, owner(), request)
	if err != nil || created.ID == first.ID || auditCount() != 2 {
		t.Fatal("retry did not commit once", created, err)
	}
	replay, err = f.Service.Connect(ctx, owner(), request)
	if err != nil || replay.ID != created.ID || auditCount() != 2 {
		t.Fatal("retry replay changed result", replay, err)
	}
}
