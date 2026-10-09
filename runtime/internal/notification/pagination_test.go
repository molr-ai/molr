package notification

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func TestIndexedPagesPreserveFiltersCountsAndExactTimeOrder(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "notices.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	all := []Notice{}
	for i, ns := range []int{0, 100000000, 120000000, 123000000, 123000001, 123000000, 999999999, 0, 100000000, 500000000} {
		at := now.Add(time.Duration(ns))
		if i%3 == 0 {
			at = at.In(time.FixedZone("offset", 8*3600))
		}
		n := Notice{ID: core.ID(fmt.Sprintf("n-%02d", i)), OwnerID: "owner", AssistantID: core.ID(fmt.Sprintf("a-%d", i%2)), Kind: []string{"task", "budget"}[i%2], CreatedAt: at, UpdatedAt: now, Version: 1}
		if i == 1 {
			n.ReadAt = &now
		}
		if i == 2 {
			n.ResolvedAt = &now
		}
		if i == 3 {
			n.DismissedAt = &now
		}
		if i == 9 {
			n.OwnerID = "other"
		}
		all = append(all, n)
	}
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, n := range all {
			if err := tx.Put(ctx, bucket, persistence.Key(n.OwnerID, n.ID), n); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	service := New(db, allow{})
	p := principal("owner")
	for _, filter := range []string{"", "all", "pending", "active", "unread"} {
		for _, kind := range []string{"", "task", "missing"} {
			for _, assistant := range []core.ID{"", "a-0"} {
				q := Query{Filter: filter, Kind: kind, AssistantID: assistant, Page: core.PageRequest{Limit: 2}}
				expected := []Notice{}
				pending, unread := 0, 0
				for _, n := range all {
					if n.OwnerID != p.OwnerID {
						continue
					}
					active := n.ResolvedAt == nil && n.DismissedAt == nil
					if active {
						pending++
						if n.ReadAt == nil {
							unread++
						}
					}
					if kind != "" && n.Kind != kind || assistant != "" && n.AssistantID != assistant {
						continue
					}
					if (filter == "active" || filter == "pending") && !active || filter == "unread" && (!active || n.ReadAt != nil) {
						continue
					}
					expected = append(expected, n)
				}
				sort.Slice(expected, func(i, j int) bool {
					if !expected[i].CreatedAt.Equal(expected[j].CreatedAt) {
						return expected[i].CreatedAt.After(expected[j].CreatedAt)
					}
					return expected[i].ID > expected[j].ID
				})
				got := []Notice{}
				seen := map[string]bool{}
				for {
					page, err := service.List(t.Context(), p, q)
					if err != nil {
						t.Fatal(err)
					}
					if page.PendingCount != pending || page.UnreadCount != unread {
						t.Fatal("counts changed with item filter/cursor", q, page)
					}
					got = append(got, page.Items...)
					if page.NextCursor == "" {
						break
					}
					if seen[page.NextCursor] {
						t.Fatal("cursor repeated")
					}
					seen[page.NextCursor] = true
					q.Page.Cursor = page.NextCursor
				}
				if len(got) != len(expected) {
					t.Fatal(q, len(got), len(expected))
				}
				for i, n := range got {
					if n.ID != expected[i].ID || !n.CreatedAt.Equal(expected[i].CreatedAt) {
						t.Fatal("order changed", q, got, expected)
					}
				}
			}
		}
	}
	first, err := service.List(t.Context(), p, Query{Filter: "active", Page: core.PageRequest{Limit: 1}})
	if err != nil || first.NextCursor == "" {
		t.Fatal(first, err)
	}
	for _, q := range []Query{{Filter: "all"}, {Filter: "active", Kind: "task"}, {Filter: "active", AssistantID: "a-0"}} {
		q.Page.Cursor = first.NextCursor
		if _, err := service.List(t.Context(), p, q); core.CodeOf(err) != core.InvalidArgument {
			t.Fatal("cursor crossed query", err)
		}
	}
}

type noticeReads struct{ records, countCalls int }
type noticeStore struct {
	persistence.Store
	reads *noticeReads
}
type noticeTx struct {
	persistence.Tx
	reads *noticeReads
}

func (s noticeStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, noticeTx{tx, s.reads}) })
}
func (tx noticeTx) Store() persistence.Store { return noticeStore{tx.Tx.Store(), tx.reads} }
func (tx noticeTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	rows, err := tx.Tx.Query(ctx, bucket, q)
	if bucket == "notifications.records" {
		tx.reads.records += len(rows)
	}
	return rows, err
}
func (tx noticeTx) Count(ctx context.Context, bucket string, q persistence.Query) (int64, error) {
	tx.reads.countCalls++
	return tx.Tx.Count(ctx, bucket, q)
}

func TestNotificationPagesDoNotLoadResolvedHistory(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "bounded.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < 1005; i++ {
			n := Notice{ID: core.ID(fmt.Sprintf("n-%04d", i)), OwnerID: "owner", Kind: "task", CreatedAt: now.Add(time.Duration(i) * time.Nanosecond), Version: 1}
			if i < 1000 {
				n.ResolvedAt = &now
			}
			if err := tx.Put(ctx, bucket, persistence.Key(n.OwnerID, n.ID), n); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, filter := range []string{"active", "all"} {
		reads := &noticeReads{}
		s := New(noticeStore{db, reads}, allow{})
		page, err := s.List(t.Context(), principal("owner"), Query{Filter: filter, Page: core.PageRequest{Limit: 2}})
		if err != nil || len(page.Items) != 2 || page.PendingCount != 5 || page.UnreadCount != 5 || reads.records != 3 || reads.countCalls != 2 {
			t.Fatal(page, err, *reads)
		}
	}
}

func TestNotificationCountAndItemMutationsRollBackTogether(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "atomic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := New(db, allow{})
	p := principal("owner")
	in := Input{Kind: "task", Code: "failed", Source: core.Object{Kind: "task", ID: "task"}}
	if err := Sync(t.Context(), db, p.OwnerID, in, true); err != nil {
		t.Fatal(err)
	}
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := Sync(ctx, db, p.OwnerID, in, false); err != nil {
			return err
		}
		page, err := s.List(ctx, p, Query{Filter: "active"})
		if err != nil || page.PendingCount != 0 || page.UnreadCount != 0 || len(page.Items) != 0 {
			t.Fatal(page, err)
		}
		return core.NewError(core.Conflict, "rollback")
	})
	if core.CodeOf(err) != core.Conflict {
		t.Fatal(err)
	}
	page, err := s.List(t.Context(), p, Query{Filter: "active"})
	if err != nil || page.PendingCount != 1 || page.UnreadCount != 1 || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
}
