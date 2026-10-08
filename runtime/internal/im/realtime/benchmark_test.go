package realtime

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

// Compare caught-up reads against the previous whole-bucket scan. Synthetic
// data includes another owner, so unrelated history must not affect tail reads.
func BenchmarkReplayTail(b *testing.B) {
	for _, count := range []int{10_000, 100_000, 1_000_000} {
		b.Run(fmt.Sprintf("records_%d", count), func(b *testing.B) {
			db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "events.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			err = db.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
				for i := 0; i < count; i++ {
					owner := core.ID("owner")
					if i%2 != 0 {
						owner = "other"
					}
					e := Visible{ID: core.ID(fmt.Sprintf("e%07d", i)), OwnerID: owner, Sequence: uint64(i/2 + 1), Topic: "message.created", Object: core.Object{Kind: "message", ID: "message"}, Version: 1, CreatedAt: time.Unix(1, 0).UTC()}
					if err := tx.Put(ctx, visibleBucket, persistence.Key(owner, e.ID), e); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				b.Fatal(err)
			}
			after, _ := Cursor("owner", nil, uint64(count/2))
			store := NewStore(db)
			b.Run("indexed", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					page, err := store.Replay(b.Context(), "owner", Query{After: after, Page: core.PageRequest{Limit: 100}})
					if err != nil || len(page.Items) != 0 {
						b.Fatal(page, err)
					}
				}
			})
			b.Run("previous_scan", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					err := db.Read(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
						rows, err := tx.Scan(ctx, visibleBucket)
						if err != nil {
							return err
						}
						for _, row := range rows {
							e, err := persistence.Decode[Visible](row)
							if err != nil {
								return err
							}
							if e.OwnerID == "owner" && e.Sequence > uint64(count/2) {
								b.Fatal("unexpected event")
							}
						}
						return nil
					})
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
