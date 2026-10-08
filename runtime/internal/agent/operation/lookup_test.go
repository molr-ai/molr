package operation

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
)

func seedLookupHistory(tb testing.TB, ctx context.Context, db persistence.Store, n int) Operation {
	tb.Helper()
	op := Operation{OwnerID: "owner", AssistantID: "assistant", ExecutionID: "run", Action: "execute", State: Succeeded, InputDigest: "digest", Version: 1, CreatedAt: time.Unix(100, 0).UTC(), UpdatedAt: time.Unix(100, 0).UTC(), Result: json.RawMessage(`{"text":"` + strings.Repeat("x", 256) + `"}`)}
	err := db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < n; i++ {
			op.ID = core.ID(fmt.Sprintf("history-%06d", i))
			op.IdempotencyKey = string(op.ID)
			if err := tx.Put(ctx, bucket, persistence.Key(op.OwnerID, op.ID), op); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	return op
}

func TestIdempotencyLookupBoundsHistoryAndIsolatesOwners(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "lookup.db"))
	testkit.RequireNoError(t, err)
	defer db.Close()
	op := seedLookupHistory(t, t.Context(), db, 1000)
	repo := NewRepository(db)
	// The same key is valid for different owners, including non-identifier keys.
	op.ID, op.IdempotencyKey = "target", "key/'quoted?"
	testkit.RequireNoError(t, repo.Insert(t.Context(), op))
	op.OwnerID, op.ID = "other", "foreign"
	testkit.RequireNoError(t, repo.Insert(t.Context(), op))
	observed := &testkit.ObservedStore{Store: db}
	repo = NewRepository(observed)
	for _, tc := range []struct{ owner, key, want string }{
		{"owner", op.IdempotencyKey, "target"},
		{"other", op.IdempotencyKey, "foreign"},
		{"absent", op.IdempotencyKey, ""},
		{"owner", "missing", ""},
	} {
		got, err := repo.FindByKey(t.Context(), core.ID(tc.owner), tc.key)
		if tc.want == "" {
			testkit.RequireCode(t, err, core.NotFound)
		} else {
			testkit.RequireNoError(t, err)
			if got.ID != core.ID(tc.want) || got.OwnerID != core.ID(tc.owner) || string(got.Result) != string(op.Result) {
				t.Fatalf("unexpected lookup: %+v", got)
			}
		}
	}
	if observed.Scans != 0 || observed.Rows[bucket] != 2 {
		t.Fatalf("unrelated history loaded: %+v", observed)
	}
	// Both Begin and Insert must use bounded lookups, including misses.
	service := NewService(observed, Options{Authorizer: fixtureAuthorizer{}})
	req := BeginRequest{Execution: core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: "assistant", OperationID: "new"}, Mutation: core.Mutation{IdempotencyKey: "new-key"}, Action: "execute", InputDigest: "digest"}
	first, err := service.Begin(t.Context(), req)
	testkit.RequireNoError(t, err)
	again, err := service.Begin(t.Context(), req)
	testkit.RequireNoError(t, err)
	if first.ID != again.ID {
		t.Fatal("duplicate operation created")
	}
	req.InputDigest = "changed"
	_, err = service.Begin(t.Context(), req)
	testkit.RequireCode(t, err, core.Conflict)
	if observed.Scans != 0 || observed.Rows[bucket] != 4 {
		t.Fatalf("Begin loaded unrelated history: %+v", observed)
	}
}

func TestConcurrentBeginDeduplicatesIdempotencyKey(t *testing.T) {
	f := newContractFixture(t)
	const count = 12
	var wg sync.WaitGroup
	start := make(chan struct{})
	ids, errors := make([]core.ID, count), make([]error, count)
	for i := range count {
		wg.Go(func() {
			<-start
			req := f.Begin
			req.Execution.OperationID = core.ID(fmt.Sprintf("candidate-%d", i))
			op, err := f.Service.Begin(t.Context(), req)
			ids[i], errors[i] = op.ID, err
		})
	}
	close(start)
	wg.Wait()
	for i := range count {
		testkit.RequireNoError(t, errors[i])
		if ids[i] == "" || ids[i] != ids[0] {
			t.Fatalf("concurrent duplicate: %v", ids)
		}
	}
	page, err := f.Repository.List(t.Context(), f.Owner.OwnerID, Query{})
	testkit.RequireNoError(t, err)
	if len(page.Items) != 1 {
		t.Fatalf("created %d operations", len(page.Items))
	}
}

func BenchmarkIdempotencyLookupHistory(b *testing.B) {
	for _, n := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "lookup.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			target := seedLookupHistory(b, b.Context(), db, n)
			repo := NewRepository(db)
			for _, hit := range []bool{true, false} {
				key, label := "missing", "miss"
				if hit {
					key, label = target.IdempotencyKey, "hit"
				}
				for _, indexed := range []bool{false, true} {
					method := "scan"
					if indexed {
						method = "indexed"
					}
					b.Run(label+"/"+method, func(b *testing.B) {
						b.ReportAllocs()
						for b.Loop() {
							var found bool
							if indexed {
								op, err := repo.FindByKey(b.Context(), target.OwnerID, key)
								if err != nil && !persistence.Missing(err) {
									b.Fatal(err)
								}
								found = err == nil && op.ID == target.ID
							} else {
								rows, err := persistence.Scan[Operation](b.Context(), db, bucket)
								if err != nil {
									b.Fatal(err)
								}
								for _, op := range rows {
									if op.OwnerID == target.OwnerID && op.IdempotencyKey == key {
										found = op.ID == target.ID
										break
									}
								}
							}
							if found != hit {
								b.Fatal("incorrect lookup result")
							}
						}
					})
				}
			}
		})
	}
}
