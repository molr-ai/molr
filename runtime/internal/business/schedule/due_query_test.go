package schedule

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
	"path/filepath"
	"testing"
	"time"
)

func TestDueQueryBoundsHistoryAndPreservesExactInstants(t *testing.T) {
	db := fixtureDB(t)
	now := time.Date(2026, 10, 9, 1, 0, 0, 123456789, time.UTC)
	before, after := now.Add(-time.Nanosecond), now.Add(time.Nanosecond)
	offset := now.In(time.FixedZone("offset", 8*3600))
	err := db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < 1000; i++ {
			at := now.Add(time.Hour)
			v := Schedule{ID: core.ID(fmt.Sprint("history-", i)), OwnerID: "owner", NextAt: &at, Enabled: i%2 == 0}
			if err := NewRepository(db).Insert(ctx, v); err != nil {
				return err
			}
		}
		for _, v := range []Schedule{
			{ID: "before", OwnerID: "owner", Enabled: true, NextAt: &before, Version: 1},
			{ID: "offset", OwnerID: "other", Enabled: true, NextAt: &offset},
			{ID: "after", OwnerID: "owner", Enabled: true, NextAt: &after},
			{ID: "disabled", OwnerID: "owner", NextAt: &before},
			{ID: "nil", OwnerID: "owner", Enabled: true},
		} {
			if err := NewRepository(db).Insert(ctx, v); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	observed := &testkit.ObservedStore{Store: db}
	repo := NewRepository(observed)
	rows, err := repo.Due(t.Context(), now, 1)
	if err != nil || len(rows) != 1 || rows[0].ID != "before" {
		t.Fatal(rows, err)
	}
	rows, err = repo.Due(t.Context(), now, 200)
	if err != nil || len(rows) != 2 || rows[1].ID != "offset" {
		t.Fatal(rows, err)
	}
	if observed.Scans != 0 || observed.Rows[bucket] != 3 {
		t.Fatal("history loaded", observed)
	}
	v := rows[0]
	v.NextAt = nil
	v.Version++
	if err = repo.Save(t.Context(), v, 1); err != nil {
		t.Fatal(err)
	}
	rows, err = repo.Due(t.Context(), now, 200)
	if err != nil || len(rows) != 1 || rows[0].ID != "offset" {
		t.Fatal("stale due key", rows, err)
	}
}

func BenchmarkDueHistory(b *testing.B) {
	for _, n := range []int{1000, 100000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "due.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
			future := now.Add(time.Hour)
			err = db.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
				for i := 0; i < n; i++ {
					v := Schedule{ID: core.ID(fmt.Sprint("history-", i)), OwnerID: "owner", Enabled: i%2 == 0, NextAt: &future}
					if err := tx.Put(ctx, bucket, persistence.Key(v.OwnerID, v.ID), record(v)); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				b.Fatal(err)
			}
			b.Run("scan", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					rows, err := persistence.Scan[Schedule](b.Context(), db, bucket)
					if err != nil {
						b.Fatal(err)
					}
					for _, v := range rows {
						if v.Enabled && v.NextAt != nil && !v.NextAt.After(now) {
							b.Fatal("unexpected due")
						}
					}
				}
			})
			b.Run("indexed", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					rows, err := NewRepository(db).Due(b.Context(), now, 200)
					if err != nil || len(rows) != 0 {
						b.Fatal(rows, err)
					}
				}
			})
		})
	}
}
