package sqlitestore

import (
	"context"
	"molr.ai/runtime/internal/persistence"
	"path/filepath"
	"testing"
	"time"
)

func TestDreamHistoryTimestampKeyTracksUpdates(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "dream.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, at := range []time.Time{time.Date(2026, 10, 1, 8, 0, 0, 123456789, time.FixedZone("offset", 8*3600)), time.Date(2026, 10, 2, 0, 0, 0, 1, time.UTC)} {
		err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, "business.dream_runs", "owner/run", struct{ CreatedAt time.Time }{at})
		})
		if err != nil {
			t.Fatal(err)
		}
		err = db.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			var row struct {
				CreatedAt    time.Time
				CreatedAtKey string
			}
			if err := tx.Get(ctx, "business.dream_runs", "owner/run", &row); err != nil {
				return err
			}
			if row.CreatedAtKey != persistence.TimeKey(at) || !row.CreatedAt.Equal(at) {
				t.Fatalf("incorrect time key: %+v", row)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
