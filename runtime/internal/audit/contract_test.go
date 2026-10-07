package audit

import (
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

type contractFixture struct {
	Service    Service
	Repository Repository
	Owner      core.Principal
	OtherOwner core.Principal
	Assistant  core.Principal
	Now        time.Time
	FailWrites func()
}

var newContractFixture func(*testing.T) contractFixture

func fixture(t *testing.T) contractFixture {
	t.Helper()
	return testkit.RequireFactory(t, "audit", newContractFixture)
}
func record(f contractFixture, id core.ID, at time.Time) Record {
	return Record{ID: id, OwnerID: f.Owner.OwnerID, SubjectID: f.Owner.SubjectID, ActorKind: core.ActorOwner, Action: "credential.put", Target: core.Object{Kind: "credential", ID: "credential"}, Outcome: Succeeded, RequestID: "request", At: at}
}
func TestTDDRecordAndAuthorizedRead(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	r := record(f, "record", f.Now)
	testkit.RequireNoError(t, f.Service.Record(ctx, r))
	p, err := f.Service.List(ctx, f.Owner, Query{Action: r.Action, Outcome: Succeeded, From: f.Now.Add(-time.Second), Until: f.Now.Add(time.Second)})
	testkit.RequireNoError(t, err)
	if len(p.Items) != 1 || p.Items[0].ID != r.ID || p.Items[0].Target != r.Target {
		t.Fatal("audit event not preserved")
	}
	p, err = f.Service.List(ctx, f.OtherOwner, Query{})
	if err == nil && len(p.Items) != 0 {
		t.Fatal("cross-owner audit leak")
	}
	_, err = f.Service.List(ctx, f.Assistant, Query{})
	if err == nil {
		t.Fatal("assistant read security audit")
	}
	p, err = f.Service.List(ctx, f.Owner, Query{Outcome: Denied})
	testkit.RequireNoError(t, err)
	if len(p.Items) != 0 {
		t.Fatal("outcome filter ignored")
	}
}
func TestTDDAuditFailuresAreReported(t *testing.T) {
	f := fixture(t)
	f.FailWrites()
	if err := f.Service.Record(testkit.Context(t), record(f, "record", f.Now)); err == nil {
		t.Fatal("audit write failure swallowed")
	}
}
func TestTDDRepositoryAppendPaginationAndRetention(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	r := f.Repository
	for i, id := range []core.ID{"old", "middle", "new"} {
		testkit.RequireNoError(t, r.Append(ctx, record(f, id, f.Now.Add(time.Duration(i-2)*time.Hour))))
	}
	p, err := r.List(ctx, f.Owner.OwnerID, Query{Page: core.PageRequest{Limit: 1}})
	testkit.RequireNoError(t, err)
	if len(p.Items) != 1 || p.NextCursor == "" {
		t.Fatal("pagination omitted cursor")
	}
	next, err := r.List(ctx, f.Owner.OwnerID, Query{Page: core.PageRequest{Limit: 1, Cursor: p.NextCursor}})
	testkit.RequireNoError(t, err)
	if len(next.Items) != 1 || next.Items[0].ID == p.Items[0].ID {
		t.Fatal("pagination repeated first row")
	}
	foreign, err := r.List(ctx, f.OtherOwner.OwnerID, Query{})
	testkit.RequireNoError(t, err)
	if len(foreign.Items) != 0 {
		t.Fatal("repository crossed owner boundary")
	}
	n, err := r.Prune(ctx, f.Now.Add(-30*time.Minute), 1)
	testkit.RequireNoError(t, err)
	if n != 1 {
		t.Fatal("retention ignored batch bound")
	}
	p, err = r.List(ctx, f.Owner.OwnerID, Query{})
	testkit.RequireNoError(t, err)
	if len(p.Items) != 2 {
		t.Fatal("retention removed wrong number")
	}
	keptRecent := false
	for _, entry := range p.Items {
		if entry.ID == "new" {
			keptRecent = true
		}
	}
	if !keptRecent {
		t.Fatal("retention removed a record newer than the cutoff")
	}
}
