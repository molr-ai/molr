package sqlitestore

import (
	"context"
	"errors"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncodedBytesRespectsScopeAndOriginalEncoding(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "sizes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	err = s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for key, value := range map[string]any{
			"alice/1": map[string]any{"Text": "中文🙂", "Rank": 3},
			"alice/2": map[string]any{"Text": "<escaped>&\n", "Rank": 1},
			"alice/3": map[string]any{"Text": "different length", "Rank": 2},
			"bob/1":   map[string]any{"Text": "foreign", "Rank": 4},
		} {
			if err := tx.Put(ctx, "sizes", key, value); err != nil {
				return err
			}
		}
		return tx.Put(ctx, "another", "alice/1", map[string]any{"Text": "another bucket"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []persistence.Query{
		{}, {Prefix: "alice/"}, {Prefix: "missing/"},
		{Prefix: "alice/", OrderBy: "Rank", Descending: true, Limit: 1, Fields: []string{"Rank"}},
		{Where: []persistence.Filter{{Field: "Rank", Op: ">", Value: 1}}, OrderBy: "Rank", After: []any{2, "alice/3"}, Limit: 1},
	} {
		err = s.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			got, err := tx.EncodedBytes(ctx, "sizes", q)
			if err != nil {
				return err
			}
			full := q
			full.Fields = nil
			rows, err := tx.Query(ctx, "sizes", full)
			if err != nil {
				return err
			}
			var want int64
			for _, r := range rows {
				want += int64(len(r.Data))
			}
			if got != want {
				t.Fatalf("query=%+v got=%d want=%d", q, got, want)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	err = s.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		_, err := tx.EncodedBytes(canceled, "sizes", persistence.Query{})
		return err
	})
	if core.CodeOf(err) != core.Canceled {
		t.Fatalf("cancellation lost: %v", err)
	}
}
func TestEncodedBytesObservesTransactionAndRollback(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "transaction.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	stop := errors.New("roll back")
	err = s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "sizes", "one", "中文"); err != nil {
			return err
		}
		got, err := tx.EncodedBytes(ctx, "sizes", persistence.Query{})
		if err != nil {
			return err
		}
		if got != 8 {
			t.Fatalf("UTF-8 JSON bytes=%d, want 8", got)
		}
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatal(err)
	}
	err = s.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		got, err := tx.EncodedBytes(ctx, "sizes", persistence.Query{})
		if got != 0 {
			t.Fatal("rolled-back bytes counted", got)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestProjectedRecordIncludesOriginalEncodedBytes(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "sizes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	err = s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, "sizes", "one", map[string]any{"Text": "中文🙂<>&", "Rank": 3})
	})
	if err != nil {
		t.Fatal(err)
	}
	err = s.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		full, err := tx.Query(ctx, "sizes", persistence.Query{})
		if err != nil {
			return err
		}
		projected, err := tx.Query(ctx, "sizes", persistence.Query{Fields: []string{"Rank"}, IncludeEncodedBytes: true})
		if err != nil {
			return err
		}
		if len(projected) != 1 || projected[0].EncodedBytes != int64(len(full[0].Data)) || string(projected[0].Data) != `{"Rank":3}` {
			t.Fatalf("bad projected size: %+v", projected)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestContextUsageQueriesUseMetadataIndexes(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "indexes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	queries := []struct {
		q     persistence.Query
		index string
	}{
		{persistence.Query{Unordered: true, IncludeEncodedBytes: true, Fields: []string{"Kind", "Parent", "Trees", "Values"}, Where: []persistence.Filter{{Field: "key", Op: "in", Value: []string{"one", "two"}}}}, "context_object_usage"},
	}
	for _, tc := range queries {
		query, args, err := querySQL("context.objects", tc.q)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := s.db.Query("EXPLAIN QUERY PLAN "+query, args...)
		if err != nil {
			t.Fatal(err)
		}
		var plan string
		for rows.Next() {
			var a, b, c int
			var detail string
			if err := rows.Scan(&a, &b, &c, &detail); err != nil {
				t.Fatal(err)
			}
			plan += detail
		}
		rows.Close()
		if !strings.Contains(plan, tc.index) {
			t.Fatalf("metadata index not used: %s", plan)
		}
	}
	for _, query := range []string{
		"SELECT SUM(length(CAST(value AS BLOB))) FROM records WHERE bucket=?",
		"SELECT COUNT(*) FROM records WHERE bucket=? AND json_extract(value, '$.Kind')='message'",
	} {
		rows, err := s.db.Query("EXPLAIN QUERY PLAN "+query, "context.objects")
		if err != nil {
			t.Fatal(err)
		}
		var plan string
		for rows.Next() {
			var a, b, c int
			var detail string
			if err := rows.Scan(&a, &b, &c, &detail); err != nil {
				t.Fatal(err)
			}
			plan += detail
		}
		rows.Close()
		if !strings.Contains(plan, "context_global_usage") {
			t.Fatalf("global index not used: %s", plan)
		}
	}
}
