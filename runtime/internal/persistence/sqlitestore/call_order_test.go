package sqlitestore

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/persistence"
)

func TestCallAccountingIndexDerivesOnlyOperationTimeKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "order.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 9, 8, 0, 0, 123456789, time.FixedZone("offset", 8*3600))
	raw, _ := json.Marshal(map[string]any{"ID": "operation", "OwnerID": "owner", "CreatedAt": at})
	// Model installing the index over a store whose other index keys already exist.
	if _, err := db.db.Exec("INSERT INTO records(bucket,key,value) VALUES(?,?,?)", "agent.operations", "owner/operation", raw); err != nil {
		t.Fatal(err)
	}
	sentinel := `{"CreatedAt":"unchanged"}`
	if _, err := db.db.Exec("INSERT INTO records(bucket,key,value) VALUES(?,?,?)", "other.records", "sentinel", sentinel); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("DELETE FROM records WHERE bucket='system.query_indexes' AND key='call-accounting-v1'"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		db, err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		err = db.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			var row struct {
				CreatedAt    time.Time
				CreatedAtKey string
			}
			if err := tx.Get(ctx, "agent.operations", "owner/operation", &row); err != nil {
				return err
			}
			if !row.CreatedAt.Equal(at) || row.CreatedAtKey != persistence.TimeKey(at) {
				t.Fatal("lost exact instant", row)
			}
			var marker bool
			if err := tx.Get(ctx, "system.query_indexes", "call-accounting-v1", &marker); err != nil {
				return err
			}
			if !marker {
				t.Fatal("missing index marker")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		var kept string
		if err := db.db.QueryRow("SELECT value FROM records WHERE bucket='other.records' AND key='sentinel'").Scan(&kept); err != nil || kept != sentinel {
			t.Fatal("unrelated data changed", kept, err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
