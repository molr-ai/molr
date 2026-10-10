package sqlitestore

import (
	"molr.ai/runtime/internal/persistence"
	"path/filepath"
	"strings"
	"testing"
)

func TestPollingQueriesUseBoundedIndexes(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "polling.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	scope := []persistence.Filter{persistence.Eq("OwnerID", "owner"), persistence.Eq("AssistantID", "assistant")}
	for _, tc := range []struct {
		name, bucket string
		q            persistence.Query
		expressions  int
	}{
		{"running", "sandbox.shell_tasks", persistence.Query{Where: append(append([]persistence.Filter{}, scope...), persistence.Eq("State", "running")), OrderBy: "StartedAtKey", Descending: true}, 3},
		{"history", "sandbox.shell_tasks", persistence.Query{Where: append(scope, persistence.Filter{Field: "State", Op: "!=", Value: "running"}), OrderBy: "StartedAtKey", Descending: true, Limit: 50}, 2},
		{"weixin-cancel", "weixin.deliveries", persistence.Query{Unordered: true, Prefix: "owner/", Where: []persistence.Filter{persistence.Eq("BindingID", "binding"), {Field: "State", Op: "in", Value: []string{"pending", "waiting", "sending"}}}}, 2},
		{"latest", "im.messages", persistence.Query{Where: []persistence.Filter{{Field: "key", Op: "in", Value: []string{"owner/a", "owner/b"}}, {Field: "ConversationID", Op: "in", Value: []string{"a", "b"}}}, Unordered: true, Limit: 2}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sql, args, err := querySQL(tc.bucket, tc.q)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := s.db.Query("EXPLAIN QUERY PLAN "+sql, args...)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			searches := 0
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
				if strings.Contains(detail, "SEARCH records") {
					searches++
					if !strings.Contains(detail, "key=?") && strings.Count(detail, "<expr>=?") < tc.expressions {
						t.Fatal("insufficiently scoped search", detail)
					}
				}
			}
			if searches == 0 {
				t.Fatal("no indexed search")
			}
		})
	}
}
