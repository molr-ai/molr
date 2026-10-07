package sqlitestore

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/persistence"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndexedQueriesPaginationProjectionAndPlan(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "query.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	err = s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < 200; i++ {
			for _, owner := range []string{"alice", "bob"} {
				if err := tx.Put(ctx, "im.messages", fmt.Sprintf("%s/m%03d", owner, i), map[string]any{"ID": i, "ConversationID": "chat", "Sequence": i, "Content": "private"}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	q := persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerKey", "alice"), persistence.Eq("ConversationID", "chat"), {Field: "Sequence", Op: ">", Value: 100}}, OrderBy: "Sequence", Descending: true, Limit: 2, Fields: []string{"Sequence"}}
	rows, err := persistence.Select[struct {
		Sequence int
		Content  string
	}](t.Context(), s, "im.messages", q)
	if err != nil || len(rows) != 2 || rows[0].Sequence != 199 || rows[1].Sequence != 198 || rows[0].Content != "" {
		t.Fatal(rows, err)
	}
	sql, args, err := querySQL("im.messages", q)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := s.db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+sql, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	found := false
	for plan.Next() {
		var id, parent, unused int
		var detail string
		if err := plan.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		t.Log(detail)
		if strings.Contains(detail, "records_query_11") && strings.Contains(detail, "SEARCH") {
			found = true
		}
		if strings.Contains(detail, "TEMP B-TREE") {
			t.Fatalf("pagination sorted outside index: %s", detail)
		}
	}
	if !found {
		t.Fatal("message composite index not used")
	}
	for _, bad := range []persistence.Query{{Where: []persistence.Filter{persistence.Eq("OwnerID') OR 1=1 --", "alice")}}, {Where: []persistence.Filter{{Field: "OwnerID", Op: "OR 1=1", Value: "alice"}}}, {Fields: []string{"x'); DROP TABLE records;--"}}} {
		if _, _, err := querySQL("im.messages", bad); err == nil {
			t.Fatal("unsafe query accepted")
		}
	}
}

func TestQuotaQueryPlans(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "plans.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, tc := range []struct{ bucket, resource, state string }{{"security.reservations", "ModelID", "Reservation.State"}, {"agent.operations", "Action", "State"}, {"plugins.http-calls", "Connection.ID", "State"}} {
		q := persistence.Query{Unordered: true, Where: []persistence.Filter{persistence.Eq("OwnerID", "owner"), persistence.Eq(tc.resource, "resource")}, Any: [][]persistence.Filter{{{Field: "CreatedAt", Op: ">=", Value: "2026-10-01T00:00:00Z"}, {Field: "CreatedAt", Op: "<", Value: "2026-11-01T00:00:00Z"}}, {persistence.Eq(tc.state, "unknown")}}}
		sql, args, err := querySQL(tc.bucket, q)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := s.db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+sql, args...)
		if err != nil {
			t.Fatal(err)
		}
		indexes := 0
		for rows.Next() {
			var a, b, c int
			var detail string
			if err := rows.Scan(&a, &b, &c, &detail); err != nil {
				t.Fatal(err)
			}
			t.Log(tc.bucket, detail)
			if strings.Contains(detail, "records_query_") {
				indexes++
			}
			if strings.Contains(detail, "sqlite_autoindex_records_1 (bucket=?)") {
				t.Fatal("quota scans whole bucket", detail)
			}
		}
		rows.Close()
		if indexes < 2 {
			t.Fatal("period and pending branches must use composite indexes", indexes)
		}
	}
}
