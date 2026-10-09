package sqlitestore

import (
	"context"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// Ancestors keeps traversal inside SQLite; limit also bounds malformed cycles.
// The repository verifies the expected length, root and content identities.
func (tx *transaction) Ancestors(ctx context.Context, bucket, key, parent string, limit int) ([]persistence.Record, error) {
	if err := tx.check(ctx); err != nil {
		return nil, err
	}
	if err := valid(bucket, key); err != nil {
		return nil, err
	}
	if !fieldName.MatchString(parent) || len(parent) > 128 || limit < 1 || limit > 1_000_000 {
		return nil, core.Invalid("ancestors", "valid parent field and bounded limit required")
	}
	rows, err := tx.sql.QueryContext(ctx, `WITH RECURSIVE chain(key,value,depth) AS (
 SELECT key,value,0 FROM records WHERE bucket=? AND key=?
 UNION ALL
 SELECT r.key,r.value,c.depth+1 FROM records r JOIN chain c
 ON r.bucket=? AND r.key=json_extract(c.value, ?) WHERE c.depth+1<?
 ) SELECT key,value FROM chain ORDER BY depth`, bucket, key, bucket, "$."+parent, limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := []persistence.Record{}
	for rows.Next() {
		var r persistence.Record
		if err = rows.Scan(&r.Key, &r.Data); err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, r)
	}
	return out, translate(ctx, rows.Err())
}
