package sqlitestore

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

func TestCountRespectsScopeLimitAndTransactions(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "count.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, key := range []string{"a/1", "a/2", "a/3", "b/1"} {
			if err := tx.Put(ctx, "count.rows", key, map[string]any{"OwnerID": key[:1], "Value": 1}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	check := func(q persistence.Query, want int64) {
		t.Helper()
		err := db.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			got, err := tx.Count(ctx, "count.rows", q)
			if err == nil && got != want {
				t.Errorf("count %d want %d", got, want)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	check(persistence.Query{}, 4)
	check(persistence.Query{Prefix: "a/"}, 3)
	check(persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "a")}, Limit: 2}, 2)
	check(persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "missing")}}, 0)
	check(persistence.Query{Prefix: "a/", OrderBy: "key", After: []any{"a/1"}}, 2)
	check(persistence.Query{Any: [][]persistence.Filter{{persistence.Eq("OwnerID", "a")}, {persistence.Eq("Value", 1)}}}, 4)
	rollback := errors.New("rollback")
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "count.rows", "a/4", map[string]any{"OwnerID": "a"}); err != nil {
			return err
		}
		got, err := tx.Count(ctx, "count.rows", persistence.Query{})
		if err != nil {
			return err
		}
		if got != 5 {
			t.Fatal(got)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	check(persistence.Query{}, 4)
	var expired persistence.Tx
	err = db.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		expired = tx
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := tx.Count(canceled, "count.rows", persistence.Query{}); core.CodeOf(err) != core.Canceled {
			t.Fatal(err)
		}
		if _, err := tx.Count(ctx, "count.rows", persistence.Query{Limit: -1}); core.CodeOf(err) != core.InvalidArgument {
			t.Fatal(err)
		}
		if _, err := tx.Count(ctx, "count.rows", persistence.Query{Where: []persistence.Filter{persistence.Eq("invalid-field", "a")}}); core.CodeOf(err) != core.InvalidArgument {
			t.Fatal(err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := expired.Count(t.Context(), "count.rows", persistence.Query{}); core.CodeOf(err) != core.FailedPrecondition {
		t.Fatal(err)
	}
}

func TestRetentionAggregatesUseCoveringExpressionIndex(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "aggregate-plan.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var index string
	if err := db.db.QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND sql LIKE '%$.OwnerID%$.Trigger%$.Bytes%'").Scan(&index); err != nil {
		t.Fatal(err)
	}
	query, args, err := querySQL("debug.summaries", persistence.Query{Unordered: true, Where: []persistence.Filter{persistence.Eq("OwnerID", "owner"), {Field: "Trigger", Op: "!=", Value: "dream_due"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, aggregate := range []string{"COUNT(*)", "COALESCE(SUM(json_extract(value, '$.Bytes')),0)"} {
		rows, err := db.db.Query("EXPLAIN QUERY PLAN SELECT "+aggregate+" FROM ("+query+")", args...)
		if err != nil {
			t.Fatal(err)
		}
		usesIndex := false
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			t.Log(aggregate, detail)
			usesIndex = usesIndex || strings.Contains(detail, index)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !usesIndex {
			t.Fatal("retention aggregate reads historical source records", aggregate)
		}
	}
}
