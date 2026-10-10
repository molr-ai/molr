package sqlitestore

import (
	"molr.ai/runtime/internal/persistence"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginCapacityCountsUseCoveringIndexes(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "capacity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, bucket := range []string{"plugins.installations", "plugins.connections"} {
		t.Run(bucket, func(t *testing.T) {
			filters := []persistence.Filter{persistence.Eq("OwnerID", "owner"), persistence.Eq("Deleted", false)}
			if bucket == "plugins.connections" {
				filters = append(filters, persistence.Eq("PluginID", "fixture.plugin"))
			}
			query, args, err := querySQL(bucket, persistence.Query{Where: filters, Unordered: true, Limit: 32})
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
				t.Fatal("capacity query loads source records")
			}
		})
	}
}
