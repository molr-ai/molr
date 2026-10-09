package debugtrace

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

// The previous full-history algorithm is an independent oracle for the indexed
// implementation, including exact tie order and protected history above capacity.
func referenceRetention(all []Run, policy Retention, now time.Time, keep core.ID, extra int) (map[core.ID]bool, bool) {
	runs := []Run{}
	removed := map[core.ID]bool{}
	for _, r := range all {
		if r.OwnerID == "owner" && r.Trigger != "dream_due" {
			runs = append(runs, r)
		}
	}
	sort.Slice(runs, func(i, j int) bool {
		a, b := runs[i], runs[j]
		if a.StartedAt.Equal(b.StartedAt) {
			if a.FirstRevision == b.FirstRevision {
				return a.ID > b.ID
			}
			return a.FirstRevision > b.FirstRevision
		}
		return a.StartedAt.After(b.StartedAt)
	})
	protected := map[core.ID]bool{}
	n := 0
	total := extra
	for _, r := range runs {
		total += r.Bytes
		if r.State == "running" {
			protected[r.ID] = true
		} else if n < policy.MinRuns {
			protected[r.ID] = true
			n++
		}
	}
	count := len(runs)
	cutoff := now.AddDate(0, 0, -policy.RetentionDays)
	for i := len(runs) - 1; i >= 0; i-- {
		r := runs[i]
		if r.ID == keep || protected[r.ID] {
			continue
		}
		if r.StartedAt.Before(cutoff) || count > policy.MaxRuns || total > policy.MaxBytes {
			removed[r.ID] = true
			count--
			total -= r.Bytes
		}
	}
	return removed, extra > 0 && total > policy.MaxBytes
}

func TestBatchedRetentionMatchesFullHistorySemantics(t *testing.T) {
	for _, caseID := range []int{0, 1, 2, 3, 4, 5} {
		t.Run(fmt.Sprint(caseID), func(t *testing.T) {
			db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "trace.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
			s := New(db, Options{MaxRuns: []int{2000, 8, 20}[caseID%3], MaxBytes: []int{1 << 20, 900, 1}[caseID%3], MinRuns: []int{1, 3, 6}[caseID%3], Now: func() time.Time { return now }})
			var all []Run
			size := 50
			if caseID == 0 {
				size = 450
			}
			for i := 0; i < size; i++ {
				at := now.AddDate(0, 0, -(i % 12))
				if i%3 == 0 {
					at = at.In(time.FixedZone("offset", 8*3600))
				}
				run := Run{ID: core.ID(fmt.Sprintf("run-%04d", i)), OwnerID: "owner", AssistantID: core.ID(fmt.Sprintf("assistant-%d", i%3)), Trigger: "user_message", State: "succeeded", StartedAt: at, FirstRevision: uint64(i % 3), Bytes: 100 + i%5, Steps: []Step{{Sequence: 1, Revision: uint64(i % 3)}}, Source: &SourceContent{Text: "retained payload"}}
				if i%13 == 0 {
					run.OwnerID = "other"
				}
				if i%11 == 0 {
					run.Trigger = "dream_due"
				}
				if i%7 == 0 {
					run.State = "running"
				}
				all = append(all, run)
			}
			err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
				for _, r := range all {
					if err := saveRun(ctx, tx, r); err != nil {
						return err
					}
					if err := tx.Put(ctx, "debug.payloads", payloadKey(r.OwnerID, r.ID, 1), Payload{Status: "available", Data: json.RawMessage(`{"value":"kept"}`)}); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			keep := core.ID("run-0005")
			extra := 0
			if caseID%2 == 1 {
				extra = 200
			}
			policy, err := s.retention(t.Context(), db)
			if err != nil {
				t.Fatal(err)
			}
			removed, capacity := referenceRetention(all, policy, now, keep, extra)
			var pruneErr error
			err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
				pruneErr = s.prune(ctx, tx, "owner", keep, extra)
				if core.CodeOf(pruneErr) == core.FailedPrecondition {
					return nil
				}
				return pruneErr
			})
			if err != nil {
				t.Fatal(err)
			}
			if (core.CodeOf(pruneErr) == core.FailedPrecondition) != capacity {
				t.Fatal("capacity mismatch", pruneErr, capacity)
			}
			for _, r := range all {
				for _, bucket := range []string{"debug.runs", "debug.summaries", "debug.payloads"} {
					key := persistence.Key(r.OwnerID, r.ID)
					if bucket == "debug.payloads" {
						key = payloadKey(r.OwnerID, r.ID, 1)
					}
					_, err := persistence.Load[json.RawMessage](t.Context(), db, bucket, key)
					if persistence.Missing(err) != removed[r.ID] {
						t.Fatalf("%s %s deletion mismatch: %v", bucket, key, err)
					}
					if err != nil && !persistence.Missing(err) {
						t.Fatal(err)
					}
				}
			}
		})
	}
}

type retentionReads struct {
	rows, maxBatch, countCalls, sumCalls int
	fullReads                            int
}
type retentionStore struct {
	persistence.Store
	reads *retentionReads
}
type retentionTx struct {
	persistence.Tx
	reads *retentionReads
}

func (s retentionStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, retentionTx{tx, s.reads}) })
}
func (s retentionStore) Write(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, retentionTx{tx, s.reads}) })
}
func (tx retentionTx) Store() persistence.Store { return retentionStore{tx.Tx.Store(), tx.reads} }
func (tx retentionTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	rows, err := tx.Tx.Query(ctx, bucket, q)
	if bucket == "debug.summaries" {
		tx.reads.rows += len(rows)
		tx.reads.maxBatch = max(tx.reads.maxBatch, len(rows))
		if len(q.Fields) == 0 {
			tx.reads.fullReads++
		}
	}
	return rows, err
}
func (tx retentionTx) Count(ctx context.Context, bucket string, q persistence.Query) (int64, error) {
	tx.reads.countCalls++
	return tx.Tx.Count(ctx, bucket, q)
}
func (tx retentionTx) Sum(ctx context.Context, bucket string, q persistence.Query, field string) (int64, error) {
	tx.reads.sumCalls++
	return tx.Tx.Sum(ctx, bucket, q, field)
}

func TestRetentionReadsOnlyProtectedIDsAndBoundedDeletionBatches(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "bounds.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	s := New(db, Options{MaxRuns: 2000, MinRuns: 10, Now: func() time.Time { return now }})
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < 1000; i++ {
			r := Run{ID: core.ID(fmt.Sprintf("run-%04d", i)), OwnerID: "owner", AssistantID: "assistant", Trigger: "user_message", State: "succeeded", StartedAt: now.Add(time.Duration(i) * time.Second), Bytes: 100, Source: &SourceContent{Text: strings.Repeat("history ", 100)}}
			if err := saveRun(ctx, tx, r); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	reads := &retentionReads{}
	s.store = retentionStore{db, reads}
	check := func() {
		t.Helper()
		if err := s.store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return s.prune(ctx, tx, "owner", "", 1) }); err != nil {
			t.Fatal(err)
		}
	}
	check()
	if reads.rows != 10 || reads.fullReads != 0 || reads.countCalls != 1 || reads.sumCalls != 1 {
		t.Fatal("unchanged history loaded into Go", *reads)
	}
	*reads = retentionReads{}
	now = now.AddDate(0, 0, 30)
	check()
	if reads.maxBatch > 200 || reads.fullReads != 0 {
		t.Fatal("unbounded deletion candidates", *reads)
	}
	var remaining int64
	err = db.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		remaining, err = tx.Count(ctx, "debug.summaries", persistence.Query{})
		return err
	})
	if err != nil || remaining != 10 {
		t.Fatal(remaining, err)
	}
	t.Logf("no eviction: 10 protected IDs; expired cleanup: max batch %d, retained %d", reads.maxBatch, remaining)
}
