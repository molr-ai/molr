package app

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
)

type workspaceQuotaReadStore struct {
	persistence.Store
	grantRows  int
	failGrants bool
}
type workspaceQuotaReadTx struct {
	persistence.Tx
	observed *workspaceQuotaReadStore
}

func (s *workspaceQuotaReadStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error { return fn(ctx, &workspaceQuotaReadTx{tx, s}) })
}
func (t *workspaceQuotaReadTx) Store() persistence.Store { return t.observed }
func (t *workspaceQuotaReadTx) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	if bucket == "security.grants" && t.observed.failGrants {
		return nil, core.NewError(core.Unavailable, "grant query failed")
	}
	rows, err := t.Tx.Query(ctx, bucket, q)
	if bucket == "security.grants" {
		t.observed.grantRows += len(rows)
	}
	return rows, err
}
func workspaceQuotaFixture(tb testing.TB) (*workspaceQuotaReadStore, policy.Grant) {
	tb.Helper()
	db, err := sqlitestore.Open(filepath.Join(tb.TempDir(), "workspace.db"))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { db.Close() })
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	current := policy.Grant{ID: "current", OwnerID: "owner", AssistantID: "assistant", ResourceID: workspaceResourceID("local"), Actions: []policy.Action{policy.Write}, Constraints: &policy.Constraints{Storage: &policy.StorageConstraints{CapacityBytes: 2 << 30, MaxFileBytes: 1 << 20, MaxFiles: 200}}, UpdatedAt: at, Version: 1}
	err = db.Write(tb.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for i := range 1000 {
			g := current
			g.ID = core.ID(fmt.Sprintf("unrelated-%04d", i))
			g.ResourceID = core.ID(fmt.Sprintf("resource-%04d", i))
			if err := tx.Put(ctx, "security.grants", persistence.Key(g.OwnerID, g.ID), g); err != nil {
				return err
			}
		}
		old := current
		old.ID = "expired"
		old.ExpiresAt = &at
		old.UpdatedAt = at.Add(time.Hour)
		old.Constraints = &policy.Constraints{Storage: &policy.StorageConstraints{CapacityBytes: 1 << 30, MaxFileBytes: 1 << 20, MaxFiles: 100}}
		for _, g := range []policy.Grant{old, current} {
			if err := tx.Put(ctx, "security.grants", persistence.Key(g.OwnerID, g.ID), g); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	return &workspaceQuotaReadStore{Store: db}, current
}
func TestWorkspaceQuotaLoadsOnlyItsResourceGrants(t *testing.T) {
	store, _ := workspaceQuotaFixture(t)
	value, err := workspaceConstraints(t.Context(), store, "owner", "assistant", "local")
	need(t, err)
	if value == nil || value.CapacityBytes != 2<<30 {
		t.Fatal("active workspace quota lost", value)
	}
	t.Logf("workspace quota read %d grant records", store.grantRows)
	if store.grantRows != 2 {
		t.Fatal("workspace lookup loaded unrelated resources", store.grantRows)
	}
}
func BenchmarkWorkspaceQuotaResourceScope(b *testing.B) {
	store, _ := workspaceQuotaFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		value, err := workspaceConstraints(b.Context(), store, "owner", "assistant", "local")
		if err != nil || value == nil || value.CapacityBytes != 2<<30 {
			b.Fatal(value, err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(store.grantRows)/float64(b.N), "grant-records/op")
}

func TestWorkspaceQuotaRetainsLifetimeAndFallbackSelection(t *testing.T) {
	store, current := workspaceQuotaFixture(t)
	put := func(g policy.Grant) {
		t.Helper()
		need(t, store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, "security.grants", persistence.Key(g.OwnerID, g.ID), g)
		}))
	}
	check := func(want int64) {
		t.Helper()
		got, err := workspaceConstraints(t.Context(), store, "owner", "assistant", "local")
		need(t, err)
		if got == nil || got.CapacityBytes != want {
			t.Fatalf("capacity = %+v, want %d", got, want)
		}
	}
	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)
	current.Temporary = &policy.TemporaryConstraints{Until: future, Limits: policy.Constraints{Storage: &policy.StorageConstraints{CapacityBytes: 4 << 30, MaxFileBytes: 2 << 20, MaxFiles: 400}}}
	put(current)
	check(4 << 30)
	current.Temporary.Until = past
	put(current)
	check(2 << 30)
	current.ExpiresAt = &past
	put(current)
	check(1 << 30) // The other expired grant has the later UpdatedAt.
	current.UpdatedAt = current.UpdatedAt.Add(2 * time.Hour)
	current.ExpiresAt = nil
	current.RevokedAt = &past
	current.Temporary.Until = future
	put(current)
	check(2 << 30) // Revocation retains base capacity, never a temporary expansion.
	for _, kind := range []string{"readonly", "no-constraints", "no-storage"} {
		g := current
		g.ID = core.ID(kind)
		g.RevokedAt = nil
		g.UpdatedAt = g.UpdatedAt.Add(time.Hour)
		g.Temporary = nil
		switch kind {
		case "readonly":
			g.Actions = []policy.Action{policy.Read}
		case "no-constraints":
			g.Constraints = nil
		case "no-storage":
			g.Constraints = &policy.Constraints{}
		}
		put(g)
	}
	check(2 << 30)
	for _, scope := range [][3]core.ID{{"foreign", "assistant", "local"}, {"owner", "other", "local"}, {"owner", "assistant", "remote"}} {
		got, err := workspaceConstraints(t.Context(), store, scope[0], scope[1], scope[2])
		need(t, err)
		if got != nil {
			t.Fatal("unrelated workspace quota selected", scope, got)
		}
	}
	store.failGrants = true
	if _, err := workspaceConstraints(t.Context(), store, "owner", "assistant", "local"); core.CodeOf(err) != core.Unavailable {
		t.Fatal("grant error became unlimited capacity", err)
	}
}

func TestWorkspaceQuotaUsesCatalogResourceAndCallerGrantOwner(t *testing.T) {
	store, current := workspaceQuotaFixture(t)
	current.ID = "catalog-workspace"
	current.ResourceID = workspaceResourceID("local")
	current.Constraints = &policy.Constraints{Storage: &policy.StorageConstraints{CapacityBytes: 4 << 30, MaxFileBytes: 2 << 20, MaxFiles: 400}}
	need(t, store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		for _, r := range []struct {
			bucket, key string
			value       any
		}{
			{"auth.bootstrap", "owner", struct{ AccountID core.ID }{"catalog-owner"}},
			{"auth.accounts", "owner", struct{ Account auth.Account }{auth.Account{ID: "owner", GroupID: "members"}}},
			{"auth.groups", "members", auth.UserGroup{ID: "members", Resources: []auth.ResourceReference{{Kind: "sandbox", ID: "local"}}}},
		} {
			if err := tx.Put(ctx, r.bucket, r.key, r.value); err != nil {
				return err
			}
		}
		catalogGrant := current
		catalogGrant.OwnerID = "catalog-owner"
		otherAssistantGrant := current
		otherAssistantGrant.ID = "other-assistant"
		otherAssistantGrant.AssistantID = "other"
		for _, g := range []policy.Grant{current, catalogGrant, otherAssistantGrant} {
			if err := tx.Put(ctx, "security.grants", persistence.Key(g.OwnerID, g.ID), g); err != nil {
				return err
			}
		}
		return nil
	}))
	got, err := workspaceConstraints(t.Context(), store, "owner", "assistant", "local")
	need(t, err)
	if got == nil || got.CapacityBytes != 4<<30 || store.grantRows != 3 {
		t.Fatal("catalog or caller scope lost", got, store.grantRows)
	}
}
