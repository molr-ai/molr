package sqlitestore

import (
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/persistence"
)

func TestUnreadCountUsesCoveringIndex(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "unread.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := persistence.Query{Unordered: true, Where: []persistence.Filter{persistence.Eq("OwnerKey", "owner"), persistence.Eq("ConversationID", "chat"), {Field: "Sequence", Op: ">", Value: 100}, persistence.Eq("Sender", "assistant")}}
	query, args, err := querySQL("im.messages", q)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.db.Query("EXPLAIN QUERY PLAN SELECT COUNT(*) FROM ("+query+")", args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	covered := false
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		t.Log(detail)
		covered = covered || strings.Contains(detail, "USING COVERING INDEX")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !covered {
		t.Fatal("unread count loads message bodies")
	}
}
