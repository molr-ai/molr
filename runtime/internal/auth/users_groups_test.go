package auth

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

type userGroupReadStore struct {
	persistence.Store
	sharedGroupReads int
	transactions     int
	depth            int
}
type userGroupReadTx struct {
	persistence.Tx
	parent *userGroupReadStore
}

func (s *userGroupReadStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	if s.depth == 0 {
		s.transactions++
	}
	s.depth++
	defer func() { s.depth-- }()
	return s.Store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return fn(ctx, &userGroupReadTx{Tx: tx, parent: s})
	})
}
func (t *userGroupReadTx) Get(ctx context.Context, bucket, key string, out any) error {
	if bucket == "auth.groups" && key == string(DefaultGroupID) {
		t.parent.sharedGroupReads++
	}
	return t.Tx.Get(ctx, bucket, key, out)
}
func usersGroupFixture(tb testing.TB) (*Manager, core.Principal, *userGroupReadStore) {
	tb.Helper()
	db, err := sqlitestore.Open(filepath.Join(tb.TempDir(), "users.db"))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { db.Close() })
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	manager, err := NewService(db, Options{Now: func() time.Time { return now }, MasterKey: make([]byte, 32), Origin: "https://molr.example"})
	if err != nil {
		tb.Fatal(err)
	}
	_, err = manager.Initialize(tb.Context(), InitializeRequest{Username: "owner", Password: core.NewSecret("synthetic-list-owner-password-2026")})
	if err != nil {
		tb.Fatal(err)
	}
	login, err := manager.Login(tb.Context(), LoginRequest{Username: "owner", Password: core.NewSecret("synthetic-list-owner-password-2026")})
	if err != nil {
		tb.Fatal(err)
	}
	principal, err := manager.Authenticate(tb.Context(), Authentication{Session: login.Session.Token})
	if err != nil {
		tb.Fatal(err)
	}
	err = db.Write(tb.Context(), func(ctx context.Context, tx persistence.Tx) error {
		var group UserGroup
		if err := tx.Get(ctx, "auth.groups", string(DefaultGroupID), &group); err != nil {
			return err
		}
		for i := range 200 {
			group.Resources = append(group.Resources, ResourceReference{Kind: "tool", ID: core.ID(fmt.Sprintf("tool-%03d", i)), Quota: ResourceQuota{Calls: 1000, Period: "day", Timezone: "UTC"}})
		}
		if err := tx.Put(ctx, "auth.groups", string(DefaultGroupID), group); err != nil {
			return err
		}
		for i := range 100 {
			id := core.ID(fmt.Sprintf("member-%03d", i))
			a := accountRecord{Account: Account{ID: id, Username: string(id), DisplayName: string(id), GroupID: DefaultGroupID, CreatedAt: now}, PasswordHash: "synthetic listing fixture; never authenticated", Version: 1}
			if err := tx.Put(ctx, "auth.accounts", string(id), a); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	observed := &userGroupReadStore{Store: db}
	manager.store = observed
	manager.repo = NewRepository(observed)
	return manager, principal, observed
}
func TestUsersLoadsEachSharedGroupOncePerPage(t *testing.T) {
	manager, principal, observed := usersGroupFixture(t)
	page, err := manager.Users(t.Context(), principal, core.PageRequest{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 101 {
		t.Fatal("wrong user count", len(page.Items))
	}
	for _, user := range page.Items {
		if user.ID != principal.OwnerID && (user.GroupID != DefaultGroupID || user.Administrator || user.GroupName == "") {
			t.Fatal("wrong user group", user)
		}
	}
	if observed.transactions != 1 {
		t.Fatalf("user page used %d separate read transactions", observed.transactions)
	}
	if observed.sharedGroupReads > 1 {
		t.Fatalf("100 users in the same group caused %d separate group reads", observed.sharedGroupReads)
	}
}
func BenchmarkUsersSharedGroup(b *testing.B) {
	manager, principal, observed := usersGroupFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		page, err := manager.Users(b.Context(), principal, core.PageRequest{Limit: 200})
		if err != nil || len(page.Items) != 101 {
			b.Fatalf("users=%d err=%v", len(page.Items), err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(observed.sharedGroupReads)/float64(b.N), "group-reads/op")
}

func TestUsersPageGroupCacheIsScopedToOneRequest(t *testing.T) {
	manager, principal, store := usersGroupFixture(t)
	first, err := manager.Users(t.Context(), principal, core.PageRequest{Limit: 40})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 40 || first.NextCursor == "" {
		t.Fatal("first page changed")
	}
	err = store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		var group UserGroup
		if err := tx.Get(ctx, "auth.groups", string(DefaultGroupID), &group); err != nil {
			return err
		}
		group.Name = "Renamed members"
		group.Version++
		return tx.Put(ctx, "auth.groups", string(DefaultGroupID), group)
	})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[core.ID]bool{}
	for _, u := range first.Items {
		seen[u.ID] = true
	}
	cursor := first.NextCursor
	for cursor != "" {
		store.sharedGroupReads = 0
		page, err := manager.Users(t.Context(), principal, core.PageRequest{Limit: 40, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		if store.sharedGroupReads > 1 {
			t.Fatal("group read repeated within page")
		}
		for _, u := range page.Items {
			if seen[u.ID] {
				t.Fatal("duplicate user", u.ID)
			}
			seen[u.ID] = true
			if u.ID != principal.OwnerID && (u.GroupName != "Renamed members" || u.Version != 1 || u.Administrator) {
				t.Fatal("stale group or changed account metadata", u)
			}
		}
		cursor = page.NextCursor
	}
	if len(seen) != 101 {
		t.Fatal("pagination lost users", len(seen))
	}
	all, err := manager.Users(t.Context(), principal, core.PageRequest{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range all.Items {
		if u.ID != principal.OwnerID && u.GroupName != "Renamed members" {
			t.Fatal("cross-request group cache")
		}
	}
}
func TestUsersGroupCacheDoesNotHideMissingGroupsOrRevocation(t *testing.T) {
	manager, principal, store := usersGroupFixture(t)
	_, err := manager.Users(t.Context(), principal, core.PageRequest{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	err = store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Delete(ctx, "auth.groups", string(DefaultGroupID))
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := manager.Users(t.Context(), principal, core.PageRequest{Limit: 200})
	if core.CodeOf(err) != core.NotFound || len(page.Items) != 0 {
		t.Fatal("missing group produced a partial/successful response", err, len(page.Items))
	}
	if err := manager.RevokeSession(t.Context(), principal, principal.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Users(t.Context(), principal, core.PageRequest{Limit: 200}); core.CodeOf(err) != core.Unauthenticated {
		t.Fatal("revoked admin session accepted", err)
	}
}
