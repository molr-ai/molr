package sqlitestore

import (
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/persistence"
)

func TestTrajectoryIncrementalQueriesUseRevisionIndexes(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "trajectory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, assistant := range []bool{false, true} {
		for _, steps := range []bool{false, true} {
			q := persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "owner")}, Unordered: true}
			scope := 1
			if assistant {
				q.Where = append(q.Where, persistence.Eq("AssistantID", "assistant"))
				scope++
			}
			if steps {
				q.Any = [][]persistence.Filter{{{Field: "LastRevision", Op: ">", Value: 10000}}, {persistence.Eq("LastRevision", 0)}}
			} else {
				q.Where = append(q.Where, persistence.Filter{Field: "UpdatedRevision", Op: ">", Value: 10000})
			}
			sql, args, err := querySQL("debug.summaries", q)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := s.db.Query("EXPLAIN QUERY PLAN "+sql, args...)
			if err != nil {
				t.Fatal(err)
			}
			bounded := 0
			for rows.Next() {
				var a, b, c int
				var detail string
				if err := rows.Scan(&a, &b, &c, &detail); err != nil {
					t.Fatal(err)
				}
				t.Log(detail)
				if strings.Contains(detail, "SCAN records") || strings.Contains(detail, "TEMP B-TREE") {
					t.Fatal(detail)
				}
				if strings.Contains(detail, "SEARCH records") && !strings.Contains(detail, "key=?") {
					expressions := strings.Count(detail, "<expr>=?")
					if expressions < scope || !(expressions > scope || strings.Contains(detail, "<expr>>?")) {
						t.Fatal("revision not bounded by index", detail)
					}
					bounded++
				}
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			if bounded == 0 {
				t.Fatal("no revision-scoped search")
			}
		}
	}
}
