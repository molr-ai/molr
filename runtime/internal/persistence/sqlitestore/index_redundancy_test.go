package sqlitestore

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/persistence"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var redundantPrefixIndexes = []recordIndex{
	{"records_query_all_ownerid", "", []string{"OwnerID"}},
	{"records_query_security_grants_ownerid_assistantid_resourceid", "security.grants", []string{"OwnerID", "AssistantID", "ResourceID"}},
}

func benchmarkIndexStore(b testing.TB, compact bool) *Store {
	b.Helper()
	s, err := Open(filepath.Join(b.TempDir(), "indexes.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { s.Close() })
	for _, index := range redundantPrefixIndexes {
		if compact {
			_, err = s.db.Exec("DROP INDEX IF EXISTS " + index.name)
		} else {
			parts := "bucket"
			for _, field := range index.fields {
				expr, _ := fieldSQL(field)
				parts += "," + expr
			}
			where := ""
			if index.bucket != "" {
				where = " WHERE bucket='" + index.bucket + "'"
			}
			_, err = s.db.Exec("CREATE INDEX IF NOT EXISTS " + index.name + " ON records(" + parts + ")" + where)
		}
		if err != nil {
			b.Fatal(err)
		}
	}
	err = s.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := range 4000 {
			owner := fmt.Sprintf("owner-%d", i%20)
			if err := tx.Put(ctx, "security.grants", fmt.Sprintf("%s/%06d", owner, i), map[string]any{"OwnerID": owner, "AssistantID": "assistant", "ResourceID": fmt.Sprintf("r-%d", i%7), "RevokedAt": nil, "ExpiresAt": nil, "Version": 1}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	return s
}

func BenchmarkIndexPrefixWrites(b *testing.B) {
	for _, compact := range []bool{false, true} {
		b.Run(fmt.Sprint(compact), func(b *testing.B) {
			s := benchmarkIndexStore(b, compact)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				err := s.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
					for j := range 100 {
						if err := tx.Put(ctx, "security.grants", fmt.Sprintf("owner-0/%06d", j*20), map[string]any{"OwnerID": "owner-0", "AssistantID": "assistant", "ResourceID": fmt.Sprintf("r-%d", j*20%7), "RevokedAt": nil, "ExpiresAt": nil, "Version": i + 2}); err != nil {
							return err
						}
					}
					return nil
				})
				if err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(indexDatabaseBytes(b, s)), "database-bytes")
		})
	}
}
func BenchmarkIndexPrefixReads(b *testing.B) {
	for _, compact := range []bool{false, true} {
		b.Run(fmt.Sprint(compact), func(b *testing.B) {
			s := benchmarkIndexStore(b, compact)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rows, err := persistence.Select[map[string]any](b.Context(), s, "security.grants", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "owner-0"), persistence.Eq("AssistantID", "assistant"), persistence.Eq("ResourceID", "r-0")}, Limit: 20})
				if err != nil || len(rows) != 20 {
					b.Fatal(len(rows), err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(indexDatabaseBytes(b, s)), "database-bytes")
		})
	}
}

func indexDatabaseBytes(t testing.TB, s *Store) int {
	t.Helper()
	var pages, free, size int
	if err := s.db.QueryRow("PRAGMA page_count").Scan(&pages); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("PRAGMA freelist_count").Scan(&free); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("PRAGMA page_size").Scan(&size); err != nil {
		t.Fatal(err)
	}
	return (pages - free) * size
}
func TestRemovedPrefixIndexesKeepScopedPlansAndReleasePages(t *testing.T) {
	s := benchmarkIndexStore(t, false)
	before := indexDatabaseBytes(t, s)
	q := persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "owner-0"), persistence.Eq("AssistantID", "assistant"), persistence.Eq("ResourceID", "r-0")}, Limit: 20}
	previous, err := persistence.Select[map[string]any](t.Context(), s, "security.grants", q)
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range redundantPrefixIndexes {
		if _, err := s.db.Exec("DROP INDEX " + index.name); err != nil {
			t.Fatal(err)
		}
	}
	current, err := persistence.Select[map[string]any](t.Context(), s, "security.grants", q)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(previous, current) || len(current) != 20 {
		t.Fatal("indexed results changed")
	}
	for _, check := range []struct {
		bucket string
		q      persistence.Query
		index  string
	}{
		{"security.grants", q, "records_query_security_grants_ownerid_assistantid_resourceid_key"},
		{"fixture.owner", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "owner-0")}, Limit: 20}, "records_query_all_ownerid_key"},
	} {
		query, args, err := querySQL(check.bucket, check.q)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := s.db.Query("EXPLAIN QUERY PLAN "+query, args...)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for rows.Next() {
			var a, b, c int
			var detail string
			if err := rows.Scan(&a, &b, &c, &detail); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(detail, "TEMP B-TREE") || strings.Contains(detail, "SCAN records") {
				t.Fatal("query regressed", detail)
			}
			found = found || strings.Contains(detail, check.index)
			t.Log(detail)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if !found {
			t.Fatal("covering prefix index not used", check.index)
		}
	}
	after := indexDatabaseBytes(t, s)
	if after >= before {
		t.Fatalf("no pages released: %d -> %d", before, after)
	}
	t.Logf("live database bytes: %d -> %d", before, after)
}
