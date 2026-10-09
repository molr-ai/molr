package debugtrace

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

// Measure retention checking on the Record path when no data needs deletion.
// Model execution and browser rendering are intentionally outside this benchmark.
func BenchmarkTraceRetentionCheckHistory(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "trace.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
			s := New(db, Options{MaxRuns: 10000, Now: func() time.Time { return now }})
			err = db.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
				for i := 0; i < count; i++ {
					run := Run{ID: core.ID(fmt.Sprintf("run-%05d", i)), OwnerID: "owner", AssistantID: "assistant", Trigger: "user_message", State: "succeeded", StartedAt: now.Add(-time.Duration(i) * time.Second), Bytes: 1024, FirstRevision: uint64(i + 1), Source: &SourceContent{Kind: "user_message", Status: "available", Text: strings.Repeat("retained context ", 32)}}
					if err := saveRun(ctx, tx, run); err != nil {
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
			for i := 0; i < b.N; i++ {
				if err := db.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error { return s.prune(ctx, tx, "owner", "") }); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
