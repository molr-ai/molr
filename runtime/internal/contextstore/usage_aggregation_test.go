package contextstore

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

type usagePayloadObserver struct {
	persistence.Tx
	bytes int
}

func (o *usagePayloadObserver) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	rows, err := o.Tx.Query(ctx, bucket, q)
	if bucket == objects {
		for _, row := range rows {
			o.bytes += len(row.Data)
		}
	}
	return rows, err
}
func seedUsageObjects(ctx context.Context, db persistence.Store, count, size int) (int64, error) {
	var bytes int64
	err := db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		for i := range count {
			value := object{Kind: "message", Data: json.RawMessage(fmt.Sprintf("%q", strings.Repeat("x", size)))}
			raw, err := json.Marshal(value)
			if err != nil {
				return err
			}
			bytes += int64(len(raw))
			if err := tx.Put(ctx, objects, fmt.Sprintf("object-%06d", i), value); err != nil {
				return err
			}
		}
		return nil
	})
	return bytes, err
}
func TestGlobalUsageDoesNotReadContextPayloads(t *testing.T) {
	db := fixture(t)
	expected, err := seedUsageObjects(t.Context(), db, 128, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	var payloadBytes int
	err = db.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		observed := &usagePayloadObserver{Tx: tx}
		got, err := GlobalUsage(ctx, observed)
		if err != nil {
			return err
		}
		if got.Bytes != expected || got.Messages != 128 {
			t.Fatalf("wrong totals: %+v expected bytes=%d", got, expected)
		}
		payloadBytes = observed.bytes
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if payloadBytes > 16<<10 {
		t.Fatalf("summary read %d bytes of context bodies into Go", payloadBytes)
	}
}
func BenchmarkGlobalContextUsage(b *testing.B) {
	db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "usage.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	expected, err := seedUsageObjects(b.Context(), db, 1000, 16<<10)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		err = db.Read(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
			got, err := GlobalUsage(ctx, tx)
			if err == nil && (got.Bytes != expected || got.Messages != 1000) {
				b.Fatalf("wrong totals: %+v", got)
			}
			return err
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}
