package sqlitestore

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// OwnerRecordBytes measures account-attributed JSON records, excluding shared
// context objects, indexes and free database pages. Shared context is measured
// separately by the caller through its reference graph. No payload leaves SQL.
func (s *Store) OwnerRecordBytes(ctx context.Context, owner core.ID) (int64, error) {
	if err := owner.Validate(); err != nil {
		return 0, err
	}
	var size int64
	err := s.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		current := tx.(*transaction)
		err := current.sql.QueryRowContext(ctx, `SELECT COALESCE(SUM(length(CAST(value AS BLOB))),0) FROM records
   WHERE bucket != 'context.objects' AND (
    COALESCE(json_extract(value,'$.OwnerID'),json_extract(value,'$.OwnerKey'),json_extract(value,'$.AccountID'),json_extract(value,'$.Session.AccountID'),json_extract(value,'$.Account.ID')) = ?
    OR key = ? OR substr(key,1,length(?)) = ?
   )`, owner, owner, string(owner)+"/", string(owner)+"/").Scan(&size)
		return translate(ctx, err)
	})
	return size, err
}
