package notification

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

// Measure listing five active notices as resolved history grows. Browser
// polling frequency and model execution are outside this benchmark.
func BenchmarkActiveNotificationsHistory(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "notifications.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
			err = db.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
				for i := 0; i < count+5; i++ {
					id := core.ID(fmt.Sprintf("notice-%05d", i))
					notice := Notice{ID: id, OwnerID: "owner", AssistantID: "assistant", Kind: "task.failed", Code: "test", Source: core.Object{Kind: "task", ID: id}, Label: strings.Repeat("Historical notice ", 8), Severity: "warning", CreatedAt: now.Add(time.Duration(i) * time.Second), UpdatedAt: now, Version: 1}
					if i < count {
						notice.ResolvedAt = &now
					}
					if err := tx.Put(ctx, bucket, persistence.Key("owner", id), notice); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				b.Fatal(err)
			}
			service := New(db, allow{})
			p := principal("owner")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				page, err := service.List(b.Context(), p, Query{Filter: "active", Page: core.PageRequest{Limit: 50}})
				if err != nil {
					b.Fatal(err)
				}
				if len(page.Items) != 5 || page.PendingCount != 5 || page.UnreadCount != 5 {
					b.Fatal("incorrect active page", page)
				}
			}
		})
	}
}
