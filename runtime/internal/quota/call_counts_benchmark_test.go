package quota

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkCallQuotaHistory(b *testing.B) {
	for _, kind := range []string{"tool", "connection"} {
		for _, size := range []int{100, 1000, 10000} {
			b.Run(fmt.Sprintf("%s/%d", kind, size), func(b *testing.B) {
				db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "usage.db"))
				if err != nil {
					b.Fatal(err)
				}
				defer db.Close()
				bucket, resource := "agent.operations", "read"
				if kind == "connection" {
					bucket, resource = "plugins.http-calls", "connection"
				}
				now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
				err = db.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
					for i := 0; i < size+2; i++ {
						state := "succeeded"
						if i >= size {
							state = "running"
						}
						r := map[string]any{"ID": fmt.Sprint(i), "OwnerID": "owner", "Action": "read", "Connection": map[string]string{"ID": "connection"}, "State": state, "CreatedAt": now.Add(time.Duration(i) * time.Nanosecond), "Arguments": map[string]string{"text": "artificial benchmark payload"}}
						if err := tx.Put(ctx, bucket, fmt.Sprintf("owner/%08d", i), r); err != nil {
							return err
						}
					}
					return nil
				})
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					used, err := CallTotals(b.Context(), db, kind, "owner", core.ID(resource), "", now, now.Add(24*time.Hour))
					if err != nil || used.Calls != int64(size+2) || used.Active != 2 {
						b.Fatal(used, err)
					}
				}
			})
		}
	}
}
