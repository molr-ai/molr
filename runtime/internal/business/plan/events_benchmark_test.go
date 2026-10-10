package plan

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/business/schedule"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func BenchmarkEventsFirstPageManyRecurringPlans(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprintf("plans-%d", count), func(b *testing.B) {
			db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "plans.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			at := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
			err = db.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
				for i := 0; i < count; i++ {
					id := core.ID(fmt.Sprintf("plan-%04d", i))
					item := schedule.Schedule{ID: id, OwnerID: "owner", Kind: schedule.Personal, Title: string(id), Enabled: true, Version: 1, Trigger: schedule.Trigger{StartAt: at, Timezone: "UTC", Recurrence: "FREQ=DAILY", MissedPolicy: schedule.Coalesce}}
					if err := tx.Put(ctx, "business.schedules", persistence.Key("owner", id), item); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				b.Fatal(err)
			}
			coordinator := New(db, allowOwner{}, nil)
			q := EventQuery{From: at, Page: core.PageRequest{Limit: 50}}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				page, err := coordinator.Events(b.Context(), owner(), q)
				if err != nil {
					b.Fatal(err)
				}
				if len(page.Items) != 50 || page.NextCursor == "" {
					b.Fatalf("invalid first page: count=%d cursor=%q", len(page.Items), page.NextCursor)
				}
				if page.Items[0].Plan.Ref.ID != "plan-0000" || page.Items[49].Plan.Ref.ID != "plan-0049" {
					b.Fatal("incorrect tie ordering")
				}
			}
		})
	}
}
