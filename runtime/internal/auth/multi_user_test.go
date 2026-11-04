package auth

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
	"path/filepath"
	"testing"
	"time"
)

func TestMultipleUsersHaveIndependentAuthenticationAndSecurity(t *testing.T) {
	f := fixture(t)
	admin, _ := initialized(t, f)
	m := f.Service.(*Manager)
	ctx := t.Context()
	user, err := m.CreateUser(ctx, admin, CreateUserRequest{Username: "alice", DisplayName: "Alice", GroupID: DefaultGroupID, Password: core.NewSecret("alice-unique-password-2026"), Mutation: core.Mutation{IdempotencyKey: "alice"}})
	testkit.RequireNoError(t, err)
	login, err := m.Login(ctx, LoginRequest{Username: "alice", Password: core.NewSecret("alice-unique-password-2026")})
	testkit.RequireNoError(t, err)
	p, err := m.Authenticate(ctx, Authentication{Session: login.Session.Token})
	testkit.RequireNoError(t, err)
	if p.OwnerID != user.ID || p.OwnerID == admin.OwnerID {
		t.Fatal("login selected another account")
	}
	_, err = m.Login(ctx, LoginRequest{Username: "alice", Password: core.NewSecret("strong-initial-password-123!")})
	testkit.RequireCode(t, err, core.Unauthenticated)
	_, err = m.Users(ctx, p, core.PageRequest{})
	testkit.RequireCode(t, err, core.Forbidden)
	_, err = m.CreateUser(ctx, p, CreateUserRequest{})
	testkit.RequireCode(t, err, core.Forbidden)
	_, err = m.Reauthenticate(ctx, p, ReauthenticateRequest{Password: core.NewSecret("strong-initial-password-123!"), Purpose: "password.change"})
	testkit.RequireCode(t, err, core.Unauthenticated)
	proof, err := m.Reauthenticate(ctx, p, ReauthenticateRequest{Password: core.NewSecret("alice-unique-password-2026"), Purpose: "password.change"})
	testkit.RequireNoError(t, err)
	err = m.ChangePassword(ctx, admin, ChangePasswordRequest{Verification: proof, NewPassword: core.NewSecret("not-admin-new-password-2026")})
	testkit.RequireCode(t, err, core.Unauthenticated) // purpose proofs cannot cross accounts
}

func TestDisableAccountRevokesSessionsAndLinksAndProtectsLastAdmin(t *testing.T) {
	f := fixture(t)
	admin, _ := initialized(t, f)
	m := f.Service.(*Manager)
	ctx := t.Context()
	u, err := m.CreateUser(ctx, admin, CreateUserRequest{Username: "bob", GroupID: DefaultGroupID, Password: core.NewSecret("bob-password-2026-test"), Mutation: core.Mutation{IdempotencyKey: "bob"}})
	testkit.RequireNoError(t, err)
	login, err := m.Login(ctx, LoginRequest{Username: "bob", Password: core.NewSecret("bob-password-2026-test")})
	testkit.RequireNoError(t, err)
	p, err := m.Authenticate(ctx, Authentication{Session: login.Session.Token})
	testkit.RequireNoError(t, err)
	proof, err := m.Reauthenticate(ctx, p, ReauthenticateRequest{Password: core.NewSecret("bob-password-2026-test"), Purpose: "login_link.create"})
	testkit.RequireNoError(t, err)
	link, err := m.CreateLoginLink(ctx, p, CreateLoginLinkRequest{Name: "bob", TargetPath: "/account", ExpiresIn: time.Hour, Verification: proof})
	testkit.RequireNoError(t, err)
	_, err = m.UpdateUser(ctx, admin, UpdateUserRequest{ID: u.ID, GroupID: u.GroupID, Disabled: true, Mutation: core.Mutation{IdempotencyKey: "disable", ExpectedVersion: u.Version}})
	testkit.RequireNoError(t, err)
	_, err = m.Authenticate(ctx, Authentication{Session: login.Session.Token})
	testkit.RequireCode(t, err, core.Unauthenticated)
	_, err = m.RedeemLoginLink(ctx, RedeemLoginLinkRequest{Token: link.Value})
	if err == nil {
		t.Fatal("disabled account redeemed link")
	}
	_, err = m.Login(ctx, LoginRequest{Username: "bob", Password: core.NewSecret("bob-password-2026-test")})
	testkit.RequireCode(t, err, core.Unauthenticated)
	me, err := m.CurrentUser(ctx, admin)
	testkit.RequireNoError(t, err)
	_, err = m.UpdateUser(ctx, admin, UpdateUserRequest{ID: admin.OwnerID, GroupID: DefaultGroupID, Mutation: core.Mutation{IdempotencyKey: "demote", ExpectedVersion: me.Version}})
	testkit.RequireCode(t, err, core.FailedPrecondition)
}

func TestAccountStartupRejectsIncompleteMetadata(t *testing.T) {
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "runtime.db"))
	testkit.RequireNoError(t, err)
	defer func() { _ = store.Close() }()
	hash, err := hashPassword("legacy-password-2026")
	testkit.RequireNoError(t, err)
	err = store.Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "auth.accounts", "owner", accountRecord{Account: Account{ID: "original-id", Username: "legacy"}, PasswordHash: hash, Version: 3}); err != nil {
			return err
		}
		return tx.Put(ctx, "im.messages", "original-id/message", map[string]string{"text": "private history"})
	})
	testkit.RequireNoError(t, err)
	_, err = NewService(store, Options{MasterKey: make([]byte, 32), Origin: "https://molr.example"})
	testkit.RequireCode(t, err, core.FailedPrecondition)
	original, err := persistence.Load[accountRecord](t.Context(), store, "auth.accounts", "owner")
	testkit.RequireNoError(t, err)
	if original.Account.GroupID != "" {
		t.Fatal("startup changed account format")
	}
	// Authentication repositories no longer retry the old singleton layout.
	_, err = NewRepository(store).Account(t.Context())
	testkit.RequireCode(t, err, core.NotFound)
	_, err = NewRepository(store).AccountByID(t.Context(), "original-id")
	testkit.RequireCode(t, err, core.NotFound)
}

func TestMultiUserProfileAndGroupLifecycle(t *testing.T) {
	f := fixture(t)
	admin, _ := initialized(t, f)
	m := f.Service.(*Manager)
	ctx := t.Context()
	g, err := m.SaveGroup(ctx, admin, SaveGroupRequest{Group: UserGroup{Name: "Family"}, Mutation: core.Mutation{IdempotencyKey: "family"}})
	testkit.RequireNoError(t, err)
	u, err := m.CreateUser(ctx, admin, CreateUserRequest{Username: "member", GroupID: g.ID, Password: core.NewSecret("member-independent-password"), Mutation: core.Mutation{IdempotencyKey: "member"}})
	testkit.RequireNoError(t, err)
	err = m.DeleteGroup(ctx, admin, g.ID, g.Version)
	testkit.RequireCode(t, err, core.FailedPrecondition)
	login, err := m.Login(ctx, LoginRequest{Username: "member", Password: core.NewSecret("member-independent-password")})
	testkit.RequireNoError(t, err)
	p, err := m.Authenticate(ctx, Authentication{Session: login.Session.Token})
	testkit.RequireNoError(t, err)
	profile, err := m.UpdateProfile(ctx, p, "My name", nil, u.Version)
	testkit.RequireNoError(t, err)
	if profile.DisplayName != "My name" || profile.Administrator || profile.GroupID != g.ID {
		t.Fatal(profile)
	}
	_, err = m.UpdateProfile(ctx, p, "stale", nil, u.Version)
	testkit.RequireCode(t, err, core.Conflict)
	_, err = m.UpdateUser(ctx, admin, UpdateUserRequest{ID: u.ID, DisplayName: profile.DisplayName, GroupID: DefaultGroupID, Mutation: core.Mutation{IdempotencyKey: "move", ExpectedVersion: profile.Version}})
	testkit.RequireNoError(t, err)
	_, err = m.Authenticate(ctx, Authentication{Session: login.Session.Token})
	testkit.RequireCode(t, err, core.Unauthenticated)
	err = m.DeleteGroup(ctx, admin, g.ID, g.Version)
	testkit.RequireNoError(t, err)
	groups, err := m.Groups(ctx, admin)
	testkit.RequireNoError(t, err)
	for _, group := range groups {
		if group.ID == g.ID {
			t.Fatal("deleted group survived")
		}
	}
}
