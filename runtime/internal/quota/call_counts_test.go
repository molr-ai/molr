package quota

import (
	"context"
	"errors"
	"fmt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"reflect"
	"testing"
	"time"
)

type callFixture struct {
	ID, OwnerID   core.ID
	Action, State string
	Connection    struct{ ID core.ID }
	CreatedAt     time.Time
	Arguments     string
}

func TestCallCountsMatchHistoryRulesAcrossScopesAndPeriods(t *testing.T) {
	db := openLedgerStore(t, t.TempDir())
	start := time.Date(2026, 10, 9, 0, 0, 0, 123456789, time.UTC)
	end := start.Add(time.Hour)
	times := []time.Time{start.Add(-time.Nanosecond), start, start.Add(time.Nanosecond), end.Add(-time.Nanosecond), end, end.Add(time.Hour)}
	rows := []callFixture{}
	for _, owner := range []core.ID{"owner", "other"} {
		for _, action := range []string{"read", "write", "model.generate"} {
			for _, state := range []string{"prepared", "running", "succeeded", "failed", "canceled", "unknown"} {
				for i, at := range times {
					if i%2 == 0 {
						at = at.In(time.FixedZone("offset", 8*3600))
					}
					r := callFixture{ID: core.ID(fmt.Sprint(len(rows))), OwnerID: owner, Action: action, State: state, CreatedAt: at, Arguments: "private payload"}
					r.Connection.ID = core.ID(action)
					rows = append(rows, r)
				}
			}
		}
	}
	if err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, bucket := range []string{"agent.operations", "plugins.http-calls"} {
			for _, r := range rows {
				if err := tx.Put(ctx, bucket, persistence.Key(r.OwnerID, r.ID), r); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"tool", "connection"} {
		for _, owner := range []core.ID{"", "owner", "other", "missing"} {
			for _, resource := range []core.ID{"", "read", "model.generate", "missing"} {
				for _, excluded := range []core.ID{"", "7", "missing"} {
					for _, period := range [][2]time.Time{{}, {start, end}, {start, {}}, {{}, end}, {end, start}, {start, start}} {
						from, until := period[0], period[1]
						want := emptyTotals()
						for _, r := range rows {
							if owner != "" && r.OwnerID != owner || resource != "" && string(resource) != r.Action || excluded != "" && r.ID == excluded {
								continue
							}
							if kind == "tool" && (r.Action == "model.generate" || r.State == "prepared") {
								continue
							}
							active := r.State == "running" || kind == "connection" && r.State == "prepared"
							if !active && !InPeriod(r.CreatedAt, from, until) {
								continue
							}
							want.Calls++
							if active {
								want.Active++
								if !InPeriod(r.CreatedAt, from, until) {
									want.Carried++
								}
							}
							if r.State == "unknown" {
								want.Unknown++
							}
						}
						got, err := CallTotals(t.Context(), db, kind, owner, resource, excluded, from, until)
						if err != nil || !reflect.DeepEqual(got, want) {
							t.Fatalf("%s owner=%s resource=%s exclude=%s period=%v: got=%+v want=%+v err=%v", kind, owner, resource, excluded, period, got, want, err)
						}
					}
				}
			}
		}
	}
}

type countObserver struct{ records, counts int }
type countingStore struct {
	persistence.Store
	observer *countObserver
}
type countingTx struct {
	persistence.Tx
	observer *countObserver
}

func (s countingStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, countingTx{tx, s.observer}) })
}
func (tx countingTx) Store() persistence.Store { return countingStore{tx.Tx.Store(), tx.observer} }
func (tx countingTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	rows, err := tx.Tx.Query(ctx, bucket, q)
	tx.observer.records += len(rows)
	return rows, err
}
func (tx countingTx) Count(ctx context.Context, bucket string, q persistence.Query) (int64, error) {
	tx.observer.counts++
	return tx.Tx.Count(ctx, bucket, q)
}
func TestCallCountsDoNotDecodeHistoryAndJoinAdmissionTransaction(t *testing.T) {
	db := openLedgerStore(t, t.TempDir())
	at := time.Now().UTC()
	r := callFixture{ID: "call", OwnerID: "owner", Action: "read", State: "running", CreatedAt: at}
	observer := &countObserver{}
	wrapped := countingStore{db, observer}
	rollback := errors.New("rollback")
	err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "agent.operations", "owner/call", r); err != nil {
			return err
		}
		used, err := CallTotals(ctx, wrapped, "tool", "owner", "read", "", at.Add(-time.Second), at.Add(time.Second))
		if err != nil || used.Calls != 1 || used.Active != 1 || observer.records != 0 || observer.counts != 4 {
			t.Fatal(used, err, *observer)
		}
		if CheckCalls(used, 0, 1) == nil {
			t.Fatal("uncommitted reservation did not occupy capacity")
		}
		r.State = "succeeded"
		if err := tx.Put(ctx, "agent.operations", "owner/call", r); err != nil {
			return err
		}
		used, err = CallTotals(ctx, wrapped, "tool", "owner", "read", "", time.Time{}, time.Time{})
		if err != nil || used.Calls != 1 || used.Active != 0 {
			t.Fatal(used, err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	used, err := CallTotals(t.Context(), wrapped, "tool", "owner", "read", "", time.Time{}, time.Time{})
	if err != nil || used.Calls != 0 || used.Active != 0 {
		t.Fatal(used, err)
	}
	if _, err = CallTotals(t.Context(), wrapped, "invalid", "owner", "read", "", time.Time{}, time.Time{}); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = CallTotals(ctx, wrapped, "tool", "owner", "read", "", time.Time{}, time.Time{}); core.CodeOf(err) != core.Canceled {
		t.Fatal(err)
	}
}
