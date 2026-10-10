package sqlitestore

import (
	"molr.ai/runtime/internal/persistence"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQuotaGrantSelectionUsesActiveRangeIndexes(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "quota-plan.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	q := persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "owner"), persistence.Eq("AssistantID", "assistant"), persistence.Eq("ResourceID", "compute"), persistence.Eq("RevokedAt", nil)}, Limit: 1}
	for _, full := range []any{nil, false} {
		for _, expiry := range []persistence.Filter{persistence.Eq("ExpiresAtKey", ""), {Field: "ExpiresAtKey", Op: ">", Value: persistence.TimeKey(time.Now())}} {
			q.Any = append(q.Any, []persistence.Filter{persistence.Eq("Scope.FullAccess", full), expiry})
		}
	}
	statement, args, err := querySQL("security.grants", q)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+statement, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	branches := 0
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		t.Log(detail)
		if strings.Contains(detail, "SCAN records") || strings.Contains(detail, "TEMP B-TREE") {
			t.Fatal("quota selection scans or sorts history", detail)
		}
		if strings.Contains(detail, "SEARCH records") && !strings.Contains(detail, "key=?") {
			branches++
			equalities := strings.Count(detail, "<expr>=?")
			if equalities < 4 || (equalities < 5 && !strings.Contains(detail, "<expr>>?")) {
				t.Fatal("scope, revocation or expiry range is not indexed", detail)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if branches != 4 {
		t.Fatal("missing expiry/FullAccess branches", branches)
	}
}
