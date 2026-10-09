package sqlitestore

import (
	"molr.ai/runtime/internal/persistence"
	"path/filepath"
	"strings"
	"testing"
)

func TestCallAccountingUsesCoveringIndexes(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "plans.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, kind := range []string{"tool", "connection"} {
		for _, owner := range []string{"owner", ""} {
			for _, resource := range []string{"resource", ""} {
				// Tool checks are owner-scoped; only shared connection ceilings omit owner.
				if owner == "" && (kind == "tool" || resource == "") {
					continue
				}
				for _, active := range []bool{false, true} {
					bucket, field := "agent.operations", "Action"
					if kind == "connection" {
						bucket, field = "plugins.http-calls", "Connection.ID"
					}
					where := []persistence.Filter{}
					if owner != "" {
						where = append(where, persistence.Eq("OwnerID", owner))
					}
					if resource != "" {
						where = append(where, persistence.Eq(field, resource))
					}
					if kind == "tool" {
						where = append(where, persistence.Filter{Field: "Action", Op: "!=", Value: "model.generate"}, persistence.Filter{Field: "State", Op: "!=", Value: "prepared"})
					}
					if active {
						states := []string{"running"}
						if kind == "connection" {
							states = append(states, "prepared")
						}
						where = append(where, persistence.Filter{Field: "State", Op: "in", Value: states})
					} else {
						where = append(where, persistence.Filter{Field: "CreatedAtKey", Op: ">=", Value: "2026-10-09"}, persistence.Filter{Field: "CreatedAtKey", Op: "<", Value: "2026-10-10"})
					}
					where = append(where, persistence.Filter{Field: "ID", Op: "!=", Value: "excluded"})
					query, args, err := querySQL(bucket, persistence.Query{Where: where, Unordered: true})
					if err != nil {
						t.Fatal(err)
					}
					rows, err := db.db.Query("EXPLAIN QUERY PLAN SELECT COUNT(*) FROM ("+query+")", args...)
					if err != nil {
						t.Fatal(err)
					}
					covered := false
					for rows.Next() {
						var id, parent, unused int
						var detail string
						if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
							t.Fatal(err)
						}
						t.Log(kind, owner, resource, active, detail)
						covered = covered || strings.Contains(detail, "USING COVERING INDEX")
					}
					err = rows.Err()
					rows.Close()
					if err != nil {
						t.Fatal(err)
					}
					if !covered {
						t.Fatal("count requires record payloads", kind, owner, resource, active)
					}
				}
			}
		}
	}
}
