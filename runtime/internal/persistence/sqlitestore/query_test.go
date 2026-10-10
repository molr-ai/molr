package sqlitestore

import (
	"context"
	"encoding/json"
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

func TestRealtimeQueryPlansUseBoundedSequenceIndexes(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "realtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, topic := range []string{"", "message.created"} {
		filters := []persistence.Filter{persistence.Eq("OwnerID", "owner"), {Field: "Sequence", Op: ">", Value: 100}}
		want := "records_query_12"
		if topic != "" {
			filters = append(filters, persistence.Eq("Topic", topic))
			want = "records_query_13"
		}
		query, args, err := querySQL("im.visible", persistence.Query{Where: filters, OrderBy: "Sequence", Limit: 101})
		if err != nil {
			t.Fatal(err)
		}
		rows, err := s.db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+query, args...)
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
			t.Log(detail)
			if strings.Contains(detail, "SEARCH") && strings.Contains(detail, want) && strings.Contains(detail, "<expr>>?") {
				found = true
			}
			if strings.Contains(detail, "TEMP B-TREE") {
				t.Fatal("unbounded sort", detail)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if !found {
			t.Fatal("sequence range index not used", topic)
		}
	}
}

func TestDispatchCandidatesUseOrderedStateIndex(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "dispatch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	q := persistence.Query{Where: []persistence.Filter{persistence.Eq("State", "queued"), {Field: "QueueKey", Op: ">", Value: "0/"}}, OrderBy: "QueueKey", Limit: 100}
	statement, args, err := querySQL("agent.jobs", q)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+statement, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		t.Log(detail)
		if strings.Contains(detail, "records_query_14") && strings.Contains(detail, "SEARCH") {
			found = true
		}
		if strings.Contains(detail, "TEMP B-TREE") {
			t.Fatal("candidate order requires full sort", detail)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("candidate query did not use dispatch index")
	}
}

func TestBackgroundQueriesUseSelectiveIndexes(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "background.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, tc := range []struct {
		bucket, index string
		q             persistence.Query
	}{
		{"business.schedules", "records_query_17", persistence.Query{Where: []persistence.Filter{persistence.Eq("Enabled", true), {Field: "NextAtKey", Op: ">", Value: ""}, {Field: "NextAtKey", Op: "<=", Value: "2026-10-09T00:00:00.000000000Z"}}, OrderBy: "NextAtKey", Limit: 200}},
		{"business.dream_sessions_index", "records_query_18", persistence.Query{Where: []persistence.Filter{persistence.Eq("State", "active"), {Field: "DeadlineKey", Op: "<=", Value: "2026-10-09T00:00:00.000000000Z"}}, OrderBy: "DeadlineKey", Limit: 100}},
		{"business.plan-links", "records_query_19", persistence.Query{Where: []persistence.Filter{persistence.Eq("HandoffPending", true)}, Unordered: true}},
		{"business.tasks", "records_query_20", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "owner"), persistence.Eq("Plan.Kind", "todo"), persistence.Eq("Plan.ID", "plan")}, Unordered: true}},
		{"notifications.records", "records_query_21", persistence.Query{Where: []persistence.Filter{persistence.Eq("Kind", "plan.ready"), persistence.Eq("ResolvedAt", nil)}, Unordered: true}},
		{"agent.operations", "records_query_22", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "owner"), persistence.Eq("IdempotencyKey", "key")}, Unordered: true, Limit: 1}},
		{"agent.jobs", "records_query_23", persistence.Query{Where: []persistence.Filter{persistence.Eq("Event.OwnerID", "owner"), persistence.Eq("Event.AssistantID", "assistant"), persistence.Eq("ExecutionID", "turn")}, Unordered: true, Limit: 1}},
		{"agent.operations", "records_query_24", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "owner"), persistence.Eq("AssistantID", "assistant"), persistence.Eq("ExecutionID", "turn")}, Unordered: true}},
	} {
		t.Run(tc.bucket, func(t *testing.T) {
			statement, args, err := querySQL(tc.bucket, tc.q)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := s.db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+statement, args...)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			found := false
			for rows.Next() {
				var a, b, c int
				var detail string
				if err = rows.Scan(&a, &b, &c, &detail); err != nil {
					t.Fatal(err)
				}
				t.Log(detail)
				if strings.Contains(detail, "SEARCH") && strings.Contains(detail, tc.index) {
					found = true
				}
				if strings.Contains(detail, "TEMP B-TREE") {
					t.Fatal("unbounded sort", detail)
				}
			}
			if err = rows.Err(); err != nil {
				t.Fatal(err)
			}
			if !found {
				t.Fatal("selective index not used")
			}
		})
	}
}

func TestQueryProjectionPreservesJSONTypes(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "types.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	value := json.RawMessage(`{"True":true,"False":false,"Null":null,"Text":"中文\n","Array":[true,2],"Object":{"a":false},"Nested":{"Value":true},"Large":9007199254740993}`)
	err = s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return tx.Put(ctx, "types", "one", value) })
	if err != nil {
		t.Fatal(err)
	}
	err = s.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		rows, err := tx.Query(ctx, "types", persistence.Query{Fields: []string{"True", "False", "Null", "Text", "Array", "Object", "Nested.Value", "Large", "Missing"}})
		if err != nil {
			return err
		}
		var original, projected map[string]json.RawMessage
		if err := json.Unmarshal(value, &original); err != nil {
			return err
		}
		if err := json.Unmarshal(rows[0].Data, &projected); err != nil {
			return err
		}
		for key, want := range original {
			if string(projected[key]) != string(want) {
				t.Fatalf("%s changed type/value: got %s want %s", key, projected[key], want)
			}
		}
		if string(projected["Missing"]) != "null" {
			t.Fatal("missing field changed", string(rows[0].Data))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
