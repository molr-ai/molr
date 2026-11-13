package sqlite

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

func (d *DB) validateV2(ctx context.Context) error {
	previous, err := persistence.Load[struct{ Version, CheckpointVersion uint32 }](ctx, d.engine, "system", "schema")
	if err != nil {
		return err
	}
	if previous.Version != 2 || previous.CheckpointVersion != 1 {
		return core.NewError(core.FailedPrecondition, "unsupported checkpoint schema")
	}
	return nil
}

// Keep one consistent snapshot, replacing it only after integrity verification.
func (d *DB) backupBeforeMigration(ctx context.Context) error {
	temp, err := os.CreateTemp(filepath.Dir(d.path), ".migration-*.db")
	if err != nil {
		return err
	}
	path := temp.Name()
	if err = temp.Close(); err != nil {
		return err
	}
	if err = os.Remove(path); err != nil {
		return err
	}
	defer os.Remove(path)
	if err = d.engine.Snapshot(ctx, path); err != nil {
		return err
	}
	if err = checkFile(ctx, path); err != nil {
		return err
	}
	return os.Rename(path, d.path+".pre-migration.db")
}

// Migration visits bounded pages within the migration transaction. The key is
// stable while values change, and no background polling is involved.
func migrateRows(ctx context.Context, tx persistence.Tx, bucket string, visit func(persistence.Record) error) error {
	after := ""
	for {
		rows, err := tx.Query(ctx, bucket, persistence.Query{Where: []persistence.Filter{{Field: "key", Op: ">", Value: after}}, OrderBy: "key", Limit: 100})
		if err != nil {
			return err
		}
		for _, row := range rows {
			if err := visit(row); err != nil {
				return err
			}
		}
		if len(rows) < 100 {
			return nil
		}
		after = rows[len(rows)-1].Key
	}
}

// Decode old formats only here; domain code uses the current representation.
func migratePlans(ctx context.Context, tx persistence.Tx) error {
	paused := map[string]bool{}
	err := migrateRows(ctx, tx, "business.plan-links", func(row persistence.Record) error {
		var old struct {
			OwnerID core.ID
			Ref     core.Object
			Paused  bool
		}
		if err := json.Unmarshal(row.Data, &old); err != nil {
			return err
		}
		paused[string(old.OwnerID)+"/"+old.Ref.Kind+"/"+string(old.Ref.ID)] = old.Paused
		var data map[string]json.RawMessage
		if err := json.Unmarshal(row.Data, &data); err != nil {
			return err
		}
		delete(data, "Paused")
		return tx.Put(ctx, "business.plan-links", row.Key, data)
	})
	if err != nil {
		return err
	}
	for _, kind := range []string{"todo", "schedule", "watch"} {
		bucket := "business." + kind + "s"
		if kind == "watch" {
			bucket = "business.watches"
		}
		err = migrateRows(ctx, tx, bucket, func(row persistence.Record) error {
			var old struct {
				ID, OwnerID     core.ID
				Paused, Enabled bool
				Version         core.Version
				NextAt          json.RawMessage
				State           string
			}
			if err := json.Unmarshal(row.Data, &old); err != nil {
				return err
			}
			var data map[string]json.RawMessage
			if err := json.Unmarshal(row.Data, &data); err != nil {
				return err
			}
			state := "active"
			if paused[string(old.OwnerID)+"/"+kind+"/"+string(old.ID)] || kind == "watch" && old.Paused {
				state = "paused"
			}
			// Matching dispatch progress and an exhausted cursor mean natural completion.
			// Other disabled schedules stay paused; the cursor is never rewound.
			if kind == "schedule" && !old.Enabled {
				var progress struct{ Version core.Version }
				err := tx.Get(ctx, "business.schedule-progress", row.Key, &progress)
				if err != nil && !persistence.Missing(err) {
					return err
				}
				exhausted := err == nil && progress.Version == old.Version && (len(old.NextAt) == 0 || string(old.NextAt) == "null")
				if !exhausted {
					state = "paused"
				}
			}
			if kind == "watch" {
				data["RunState"], _ = json.Marshal(old.State)
			}
			data["State"], _ = json.Marshal(state)
			delete(data, "Paused")
			delete(data, "Enabled")
			return tx.Put(ctx, bucket, row.Key, data)
		})
		if err != nil {
			return err
		}
	}
	if err = migratePlanReceipts(ctx, tx); err != nil {
		return err
	}
	return migrateRows(ctx, tx, "agent.checkpoints", func(row persistence.Record) error {
		var data map[string]json.RawMessage
		if err := json.Unmarshal(row.Data, &data); err != nil {
			return err
		}
		var version uint32
		if err := json.Unmarshal(data["FormatVersion"], &version); err != nil {
			return err
		}
		if version != 1 || string(data["ContextStored"]) != "true" {
			return core.NewError(core.FailedPrecondition, "unsupported checkpoint record")
		}
		delete(data, "FormatVersion")
		return tx.Put(ctx, "agent.checkpoints", row.Key, data)
	})
}

// Unchanged create requests may replay after an upgrade; migrate their responses
// without re-running the original mutation or changing its idempotency digest.
func migratePlanReceipts(ctx context.Context, tx persistence.Tx) error {
	for _, spec := range []struct{ bucket, source string }{{"business.todos.receipts", "business.todos"}, {"business.schedules.receipts", "business.schedules"}, {"business.watch-receipts", "business.watches"}, {"business.plan-receipts", ""}} {
		err := migrateRows(ctx, tx, spec.bucket, func(record persistence.Record) error {
			var receipt map[string]json.RawMessage
			if err := json.Unmarshal(record.Data, &receipt); err != nil {
				return err
			}
			var result map[string]json.RawMessage
			if err := json.Unmarshal(receipt["Result"], &result); err != nil {
				return err
			}
			if result == nil {
				return nil
			}
			if spec.source == "" {
				if result["Ref"] == nil {
					return nil
				}
				delete(result, "Paused")
			} else {
				if result["ID"] == nil {
					return nil
				}
				var id, owner core.ID
				if err := json.Unmarshal(result["ID"], &id); err != nil {
					return err
				}
				if err := json.Unmarshal(result["OwnerID"], &owner); err != nil {
					return err
				}
				var source struct{ State string }
				err := tx.Get(ctx, spec.source, persistence.Key(owner, id), &source)
				if err != nil && !persistence.Missing(err) {
					return err
				}
				if persistence.Missing(err) {
					source.State = "deleted"
				}
				if spec.source == "business.watches" {
					result["RunState"] = result["State"]
				}
				result["State"], _ = json.Marshal(source.State)
				delete(result, "Paused")
				delete(result, "Enabled")
			}
			receipt["Result"], _ = json.Marshal(result)
			return tx.Put(ctx, spec.bucket, record.Key, receipt)
		})
		if err != nil {
			return err
		}
	}
	return nil
}
