package sqlitestore

import (
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/persistence"
)

func TestJobListQueryPlan(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, assistant := range []bool{false, true} {
		for _, after := range [][]any{nil, {"owner/job-005"}} {
			filters := []persistence.Filter{persistence.Eq("Event.OwnerID", "owner")}
			expressions := 2
			if assistant {
				filters = append(filters, persistence.Eq("Event.AssistantID", "assistant"))
				expressions++
			}
			q := persistence.Query{Where: filters, Any: [][]persistence.Filter{{{Field: "State", Op: "in", Value: []string{"queued", "retry"}}}}, After: after, Limit: 201}
			sql, args, err := querySQL("agent.jobs", q)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := s.db.Query("EXPLAIN QUERY PLAN "+sql, args...)
			if err != nil {
				t.Fatal(err)
			}
			candidates := false
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
					if strings.Count(detail, "<expr>=?") < expressions {
						t.Fatal("candidate query scans unrelated states", detail)
					}
					candidates = true
				}
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			if !candidates {
				t.Fatal("missing state-scoped candidate index")
			}
		}
	}
}
