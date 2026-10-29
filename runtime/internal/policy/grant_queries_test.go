package policy

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

type grantReadStore struct {
	persistence.Store
	rows int
}
type grantReadTx struct {
	persistence.Tx
	store *grantReadStore
}

func (s *grantReadStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, grantReadTx{tx, s}) })
}
func (tx grantReadTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	rows, err := tx.Tx.Query(ctx, bucket, q)
	if bucket == "security.grants" {
		tx.store.rows += len(rows)
	}
	return rows, err
}
func seedGrantQueries(t testing.TB, count int) (*grantReadStore, time.Time) {
	t.Helper()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "grants.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	err = db.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := 0; i < count; i++ {
			g := Grant{ID: core.ID(fmt.Sprintf("grant-%05d", i)), OwnerID: "owner", AssistantID: "assistant", ResourceID: "resource", Version: 1}
			if i%2 == 0 {
				g.RevokedAt = &now
			}
			if err := tx.Put(ctx, "security.grants", persistence.Key(g.OwnerID, g.ID), g); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return &grantReadStore{Store: db}, now
}
func TestGrantPagesBoundReadsAndBindScope(t *testing.T) {
	store, _ := seedGrantQueries(t, 105)
	repo := NewRepository(store)
	q := Query{AssistantID: "assistant", ResourceID: "resource", Page: core.PageRequest{Limit: 10}}
	var ids []core.ID
	for {
		store.rows = 0
		page, err := repo.Grants(t.Context(), "owner", q)
		if err != nil {
			t.Fatal(err)
		}
		if store.rows > 11 {
			t.Fatalf("page read %d grants", store.rows)
		}
		for _, g := range page.Items {
			ids = append(ids, g.ID)
		}
		if page.NextCursor == "" {
			break
		}
		if q.Page.Cursor == "" {
			other := q
			other.Page.Cursor = page.NextCursor
			other.AssistantID = "other"
			if _, err := repo.Grants(t.Context(), "owner", other); core.CodeOf(err) != core.InvalidArgument {
				t.Fatal("cursor accepted other scope", err)
			}
			if _, err := repo.Grants(t.Context(), "other", Query{Page: other.Page}); core.CodeOf(err) != core.InvalidArgument {
				t.Fatal("cursor accepted other owner", err)
			}
		}
		q.Page.Cursor = page.NextCursor
	}
	if len(ids) != 105 {
		t.Fatal("lost grants", len(ids))
	}
	for i, id := range ids {
		if id != core.ID(fmt.Sprintf("grant-%05d", i)) {
			t.Fatal("wrong order", i, id)
		}
	}
}
func TestActiveGrantsFiltersScopeExpiryAndObservesRevocation(t *testing.T) {
	store, now := seedGrantQueries(t, 100)
	future := now.Add(time.Second)
	past := now.Add(-time.Second)
	entries := []Grant{
		{ID: "expired", OwnerID: "owner", AssistantID: "assistant", ResourceID: "resource", ExpiresAt: &past},
		{ID: "boundary", OwnerID: "owner", AssistantID: "assistant", ResourceID: "resource", ExpiresAt: &now},
		{ID: "future", OwnerID: "owner", AssistantID: "assistant", ResourceID: "resource", ExpiresAt: &future},
		{ID: "foreign", OwnerID: "other", AssistantID: "assistant", ResourceID: "resource"},
		{ID: "assistant", OwnerID: "owner", AssistantID: "other", ResourceID: "resource"},
		{ID: "resource", OwnerID: "owner", AssistantID: "assistant", ResourceID: "other"},
	}
	if err := store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, g := range entries {
			if err := tx.Put(ctx, "security.grants", persistence.Key(g.OwnerID, g.ID), g); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	q := Query{AssistantID: "assistant", ResourceID: "resource"}
	rows, err := ActiveGrants(t.Context(), store, "owner", q, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 51 || store.rows != 51 {
		t.Fatalf("rows=%d decoded=%d", len(rows), store.rows)
	}
	if err := NewRepository(store).RevokeGrant(t.Context(), "owner", "grant-00001", 1, now); err != nil {
		t.Fatal(err)
	}
	rows, err = ActiveGrants(t.Context(), store, "owner", q, future)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 49 {
		t.Fatal("stale revocation or expiration", len(rows))
	}
}
func BenchmarkGrantFirstPage(b *testing.B) {
	store, _ := seedGrantQueries(b, 5000)
	repo := NewRepository(store)
	q := Query{AssistantID: "assistant", Page: core.PageRequest{Limit: 20}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := repo.Grants(b.Context(), "owner", q); err != nil {
			b.Fatal(err)
		}
	}
}
