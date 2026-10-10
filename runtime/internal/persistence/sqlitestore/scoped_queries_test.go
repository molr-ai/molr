package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScopedQueriesUseIndexes(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "query.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, tc := range []struct {
		name, bucket string
		q            persistence.Query
		ordered      bool
	}{
		{"permission pending", "security.permission_requests", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "owner"), persistence.Eq("AssistantID", "assistant"), persistence.Eq("State", "pending")}, OrderBy: "CreatedAtKey", Descending: true, ThenBy: "ID", Limit: 200}, true},
		{"permission decided", "security.permission_requests", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "owner"), persistence.Eq("AssistantID", "assistant"), {Field: "State", Op: "!=", Value: "pending"}}, OrderBy: "CreatedAtKey", Descending: true, ThenBy: "ID", Limit: 200}, true},
		{"model binding", "business.bindings", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerKey", "owner"), {Field: "ModelIDs", Op: "has", Value: "model-target"}}, Limit: 1, Unordered: true}, false},
		{"search conversations", "im.conversations", persistence.Query{Prefix: "owner/", Where: []persistence.Filter{persistence.Eq("OwnerID", "owner"), persistence.Eq("PeerID", "assistant")}, Any: [][]persistence.Filter{{persistence.Eq("Archived", false)}, {persistence.Eq("Archived", nil)}}, After: []any{"owner/chat"}, Limit: 200}, true},
		{"search messages", "im.messages", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerKey", "owner"), persistence.Eq("ConversationID", "chat"), {Field: "Sequence", Op: ">", Value: 200}}, OrderBy: "Sequence", Limit: 200}, true},
		{"weixin delivery status", "weixin.deliveries", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerKey", "owner"), persistence.Eq("BindingID", "binding")}, OrderBy: "CreatedAtKey", Descending: true, ThenBy: "key", Limit: 30}, true},
		{"session", "auth.sessions", persistence.Query{Where: []persistence.Filter{persistence.Eq("TokenDigest", "digest")}, Limit: 1, Unordered: true}, false},
		{"token", "auth.tokens", persistence.Query{Where: []persistence.Filter{persistence.Eq("Digest", "digest")}, Limit: 1, Unordered: true}, false},
		{"username", "auth.accounts", persistence.Query{Where: []persistence.Filter{persistence.Eq("Account.Username", "name")}, Limit: 1}, true},
		{"live execution", "agent.jobs", persistence.Query{Where: []persistence.Filter{persistence.Eq("Event.OwnerID", "owner"), persistence.Eq("Event.AssistantID", "assistant"), persistence.Eq("ExecutionID", "execution"), persistence.Eq("State", "running")}, Limit: 1, Unordered: true}, false},
		{"grant", "security.grants", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "owner"), persistence.Eq("AssistantID", "assistant"), persistence.Eq("ResourceID", "resource")}}, true},
		{"task page", "business.tasks", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "owner"), persistence.Eq("AssistantID", "assistant")}, After: []any{"owner/100"}, OrderBy: "key", Limit: 51}, true},
		{"outbox", "im.outbox", persistence.Query{OrderBy: "AtKey", ThenBy: "ID", Limit: 100}, true},
		{"acknowledge", "im.outbox", persistence.Query{Where: []persistence.Filter{persistence.Eq("ID", "event")}, Limit: 1}, true},
		{"attachment", "im.messages", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerKey", "owner"), persistence.Eq("ConversationID", "chat"), {Field: "Content.AttachmentIDs", Op: "has", Value: "file"}}, Limit: 1}, false},
		{"dependency", "business.plan-links", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "owner"), {Field: "Dependencies", Op: "has", Value: "todo/one"}}}, false},
		{"dream history", "business.dream_runs", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "owner"), persistence.Eq("AssistantID", "assistant")}, OrderBy: "CreatedAtKey", Descending: true, ThenBy: "key", Limit: 60}, true},
		{"dream evidence", "business.dream_runs", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "owner"), {Field: "Sources.Evidence", Op: "has", Value: "message/one/hash"}}, Limit: 1}, false},
		{"message time range", "im.messages", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerKey", "owner"), persistence.Eq("ConversationID", "chat"), {Field: "CreatedAtKey", Op: ">=", Value: "2026"}}, Unordered: true}, false},
		{"audit page", "security.audit", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "owner")}, OrderBy: "AtKey", Descending: true, ThenBy: "ID", Limit: 51}, true},
		{"notification page", "notifications.records", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "owner")}, OrderBy: "CreatedAtKey", Descending: true, ThenBy: "ID", ThenDescending: true, Limit: 51}, true},
		{"notification active", "notifications.records", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "owner"), persistence.Eq("ResolvedAt", nil), persistence.Eq("DismissedAt", nil)}, OrderBy: "CreatedAtKey", Descending: true, ThenBy: "ID", ThenDescending: true, Limit: 51}, true},
		{"notification unread", "notifications.records", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "owner"), persistence.Eq("ResolvedAt", nil), persistence.Eq("DismissedAt", nil), persistence.Eq("ReadAt", nil)}, OrderBy: "CreatedAtKey", Descending: true, ThenBy: "ID", ThenDescending: true, Limit: 51}, true},
		{"debug retention candidates", "debug.summaries", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "owner"), {Field: "State", Op: "!=", Value: "running"}, {Field: "Trigger", Op: "!=", Value: "dream_due"}, {Field: "StartedAtKey", Op: "<", Value: "2026"}}, OrderBy: "StartedAtKey", ThenBy: "FirstRevision", Limit: 200, Fields: []string{"ID", "AssistantID", "Bytes", "StartedAt", "FirstRevision"}}, true},
		{"debug protected", "debug.summaries", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "owner"), {Field: "State", Op: "!=", Value: "running"}, {Field: "Trigger", Op: "!=", Value: "dream_due"}}, OrderBy: "StartedAtKey", Descending: true, ThenBy: "FirstRevision", ThenDescending: true, Limit: 20}, true},
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
			for rows.Next() {
				var a, b, c int
				var detail string
				if err := rows.Scan(&a, &b, &c, &detail); err != nil {
					t.Fatal(err)
				}
				t.Log(detail)
				if strings.Contains(detail, "sqlite_autoindex_records_1 (bucket=?)") || strings.Contains(detail, "SCAN records") {
					t.Fatalf("unbounded bucket scan: %s", detail)
				}
				if tc.ordered && strings.Contains(detail, "TEMP B-TREE") {
					t.Fatalf("unbounded sort: %s", detail)
				}
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestTermIndexesTrackUpdatesRollbackDeleteAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terms.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	put := func(ctx context.Context, tx persistence.Tx, files ...string) error {
		return tx.Put(ctx, "im.messages", "owner/message", map[string]any{"ID": "message", "ConversationID": "chat", "CreatedAt": time.Now(), "Content": map[string]any{"AttachmentIDs": files}})
	}
	check := func(want int, term string) {
		t.Helper()
		rows, err := persistence.Select[map[string]any](t.Context(), s, "im.messages", persistence.Query{Where: []persistence.Filter{{Field: "Content.AttachmentIDs", Op: "has", Value: term}}})
		if err != nil || len(rows) != want {
			t.Fatalf("%s: %d %v", term, len(rows), err)
		}
	}
	if err := s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return put(ctx, tx, "a", "a") }); err != nil {
		t.Fatal(err)
	}
	check(1, "a")
	if err := s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := put(ctx, tx, "b"); err != nil {
			return err
		}
		return errors.New("rollback")
	}); err == nil {
		t.Fatal("missing rollback")
	}
	check(1, "a")
	check(0, "b")
	if err := s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error { return put(ctx, tx, "b") }); err != nil {
		t.Fatal(err)
	}
	check(0, "a")
	check(1, "b")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	check(1, "b")
	if err := s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Delete(ctx, "im.messages", "owner/message")
	}); err != nil {
		t.Fatal(err)
	}
	check(0, "b")
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM record_terms").Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}
func TestTimeKeysAndKeysetPagePreserveNanosecondsAndOffsets(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "page.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	base := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	type row struct {
		ID, OwnerID string
		At          time.Time
	}
	err = s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < 21; i++ {
			v := row{fmt.Sprintf("%02d", i), "owner", base.Add(time.Duration(i) * time.Nanosecond).In(time.FixedZone("offset", 8*3600))}
			if err := tx.Put(ctx, "security.audit", "owner/"+v.ID, v); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	request := core.PageRequest{Limit: 3}
	ids := []string{}
	for {
		page, err := persistence.Page[row](t.Context(), s, "security.audit", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", "owner")}, OrderBy: "AtKey", Descending: true, ThenBy: "ID"}, request, "test", nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range page.Items {
			ids = append(ids, r.ID)
		}
		if page.NextCursor == "" {
			break
		}
		request.Cursor = page.NextCursor
	}
	if len(ids) != 21 {
		t.Fatal(ids)
	}
	for i, id := range ids {
		if id != fmt.Sprintf("%02d", 20-i) {
			t.Fatal(ids)
		}
	}
}

func TestScopedAggregateAndDeletionRespectTransactions(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "aggregate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	err = s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < 10; i++ {
			if err := tx.Put(ctx, "storage.files", fmt.Sprintf("owner/%d", i), map[string]any{"OwnerID": "owner", "Size": i, "State": "ready"}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	q := persistence.Query{Where: []persistence.Filter{persistence.Eq("State", "ready")}}
	check := func(want int64) {
		t.Helper()
		err := s.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			sum, err := tx.Sum(ctx, "storage.files", q, "Size")
			if sum != want {
				t.Fatalf("sum %d, want %d", sum, want)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	check(45)
	if err := s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		n, err := tx.DeleteMatching(ctx, "storage.files", q)
		if err != nil {
			return err
		}
		if n != 10 {
			t.Fatal(n)
		}
		return errors.New("rollback")
	}); err == nil {
		t.Fatal("missing rollback")
	}
	check(45)
	if err := s.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		_, err := tx.DeleteMatching(ctx, "storage.files", q)
		return err
	}); err == nil {
		t.Fatal("read-only delete")
	}
	if err := s.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		_, err := tx.DeleteMatching(ctx, "storage.files", q)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	check(0)
}

func TestOpenInitializesDerivedTimeKeysWithoutChangingDomainValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE records (bucket TEXT NOT NULL,key TEXT NOT NULL,value BLOB NOT NULL,PRIMARY KEY(bucket,key))"); err != nil {
		t.Fatal(err)
	}
	// Representative pre-index data, including nil expiry and sub-millisecond times.
	samples := map[string]string{
		"security.grants": `{"OwnerID":"owner","AssistantID":"assistant","ResourceID":"resource","ExpiresAt":null,"RevokedAt":null,"Version":7}`,
		"im.messages":     `{"ID":"message","CreatedAt":"2026-10-09T08:00:00.000000123+08:00","Content":{"AttachmentIDs":["file"]}}`,
		"debug.summaries": `"invalid recording shape"`,
	}
	for bucket, raw := range samples {
		if _, err = db.Exec("INSERT INTO records VALUES(?,?,?)", bucket, "owner/item", []byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for bucket, raw := range samples {
		var stored []byte
		if err := s.db.QueryRow("SELECT value FROM records WHERE bucket=? AND key=?", bucket, "owner/item").Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if bucket == "debug.summaries" {
			if string(stored) != raw {
				t.Fatal("damaged recording rewritten")
			}
			continue
		}
		var before, after map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &before); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(stored, &after); err != nil {
			t.Fatal(err)
		}
		for key, want := range before {
			if string(after[key]) != string(want) {
				t.Fatalf("changed canonical %s.%s", bucket, key)
			}
		}
		if bucket == "security.grants" && string(after["ExpiresAtKey"]) != `""` {
			t.Fatal("nil expiry was not indexed")
		}
		if bucket == "im.messages" && string(after["CreatedAtKey"]) != `"2026-10-09T00:00:00.000000123Z"` {
			t.Fatal("timestamp lost precision or zone")
		}
	}
	// A completed initializer must not revisit historical rows on restart.
	if _, err := s.db.Exec(`CREATE TRIGGER reject_history_rewrite BEFORE UPDATE ON records WHEN OLD.bucket!='system.query_indexes' BEGIN SELECT RAISE(FAIL,'history rewritten'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
}
