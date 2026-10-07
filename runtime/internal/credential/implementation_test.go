package credential

import (
	"context"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/tests/testkit"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fixtureBinding struct {
	ID, OwnerID, CredentialID core.ID
	Origin, Provider          string
}

func init() {
	newContractFixture = func(t *testing.T) contractFixture {
		store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "credential.db"))
		testkit.RequireNoError(t, err)
		t.Cleanup(func() { _ = store.Close() })
		ctx := testkit.Context(t)
		now := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
		var mu sync.Mutex
		clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
		validateAssistant := func(ctx context.Context, p core.Principal) error {
			saved, e := persistence.Load[core.Principal](ctx, store, "security.test_executions", persistence.Key(p.OwnerID, p.ExecutionID))
			if e != nil {
				return e
			}
			if saved != p {
				return core.NewError(core.Forbidden, "execution mismatch")
			}
			return nil
		}
		authentication, err := auth.NewService(store, auth.Options{MasterKey: make([]byte, 32), Origin: "https://molr.example", Now: clock, ValidateAssistant: validateAssistant})
		testkit.RequireNoError(t, err)
		account, err := authentication.Initialize(ctx, auth.InitializeRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!")})
		testkit.RequireNoError(t, err)
		// These contracts exercise credentials with sensitive-action protection enabled.
		accountRepo := auth.NewRepository(store)
		stored, err := accountRepo.Account(ctx)
		testkit.RequireNoError(t, err)
		stored.Account.ReauthenticationEnabled = true
		testkit.RequireNoError(t, accountRepo.SaveAccount(ctx, stored, stored.Version))
		login, err := authentication.Login(ctx, auth.LoginRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!")})
		testkit.RequireNoError(t, err)
		owner, err := authentication.Authenticate(ctx, auth.Authentication{Session: login.Session.Token})
		testkit.RequireNoError(t, err)
		assistant := core.Principal{OwnerID: account.ID, Kind: core.ActorAssistant, SubjectID: "assistant", ExecutionID: "execution"}
		execution := core.Execution{Principal: assistant, AssistantID: assistant.SubjectID, OperationID: "operation"}
		testkit.RequireNoError(t, store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, "security.test_executions", persistence.Key(account.ID, assistant.ExecutionID), assistant)
		}))
		owns := func(ctx context.Context, p core.Principal, o core.Object) error {
			if p.OwnerID != account.ID {
				return core.NewError(core.Forbidden, "foreign owner")
			}
			if o.Kind == "assistant" && o.ID == assistant.SubjectID {
				return nil
			}
			if o.Kind == "resource" {
				resource, e := persistence.Load[fixtureBinding](ctx, store, "business.resources", persistence.Key(p.OwnerID, o.ID))
				if e != nil {
					return e
				}
				if resource.OwnerID == p.OwnerID {
					return nil
				}
			}
			return core.NewError(core.Forbidden, "target unavailable")
		}
		permissions, err := policy.NewService(store, policy.Options{Now: clock, ValidatePrincipal: authentication.ValidatePrincipal, CheckScope: authentication.CheckScope, Owns: owns, ResourceActions: func(context.Context, core.Principal, core.ID) ([]string, error) { return []string{"execute"}, nil }})
		testkit.RequireNoError(t, err)
		authorize := func(ctx context.Context, a Access) error {
			p, e := a.EffectivePrincipal()
			if e != nil {
				return e
			}
			binding, e := persistence.Load[fixtureBinding](ctx, store, "business.resources", persistence.Key(p.OwnerID, a.ResourceID))
			if e != nil {
				return e
			}
			value, e := NewRepository(store).Get(ctx, p.OwnerID, a.CredentialID)
			if e != nil {
				return e
			}
			if binding.CredentialID != a.CredentialID || binding.Origin != a.Origin || binding.Provider != value.Metadata.Provider {
				return core.NewError(core.Forbidden, "credential binding mismatch")
			}
			return permissions.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: a.ResourceID}, ResourceID: a.ResourceID, Action: policy.Execute, Destination: a.Origin})
		}
		vault, err := NewService(store, Options{MasterKey: make([]byte, 32), Now: clock, ValidatePrincipal: authentication.ValidatePrincipal, CheckScope: authentication.CheckScope, CheckVerification: authentication.CheckVerification, AuthorizeAccess: authorize, RefreshProvider: func(ctx context.Context, a Access, s core.Secret) (core.Secret, error) { return s, nil }})
		testkit.RequireNoError(t, err)
		var grant policy.Grant
		return contractFixture{Manager: vault, Broker: vault, Repository: NewRepository(store), Owner: owner, OtherOwner: core.Principal{OwnerID: "other", SubjectID: "other", Kind: core.ActorOwner, SessionID: "other-session"}, Assistant: assistant, Execution: execution, Verification: func(purpose string) auth.Verification {
			v, e := authentication.Reauthenticate(ctx, owner, auth.ReauthenticateRequest{Password: core.NewSecret("strong-initial-password-123!"), Purpose: purpose})
			testkit.RequireNoError(t, e)
			return v
		}, Bind: func(id core.ID) Access {
			testkit.RequireNoError(t, store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				return tx.Put(ctx, "business.resources", persistence.Key(account.ID, "resource"), fixtureBinding{ID: "resource", OwnerID: account.ID, CredentialID: id, Origin: "https://approved.example", Provider: "test-provider"})
			}))
			expiry := clock().Add(time.Hour)
			var e error
			grant, e = permissions.Grant(ctx, owner, policy.GrantRequest{Mutation: core.Mutation{IdempotencyKey: "grant"}, AssistantID: assistant.SubjectID, ResourceID: "resource", Actions: []policy.Action{policy.Execute}, ExpiresAt: &expiry})
			testkit.RequireNoError(t, e)
			return Access{Execution: execution, ResourceID: "resource", CredentialID: id, Origin: "https://approved.example"}
		}, RevokeAccess: func(a Access) {
			testkit.RequireNoError(t, permissions.Revoke(ctx, owner, policy.RevokeRequest{Mutation: core.Mutation{IdempotencyKey: "revoke", ExpectedVersion: grant.Version}, GrantID: grant.ID}))
		}, ExpireAccess: func(Access) { mu.Lock(); now = now.Add(2 * time.Hour); mu.Unlock() }}
	}
}
func TestCredentialOwnerConnectionCheckUsesOwnPrincipal(t *testing.T) {
	f := fixture(t)
	c := put(t, f)
	a := f.Bind(c.ID)
	a.Principal = f.Owner
	a.Execution = core.Execution{}
	called := false
	testkit.RequireNoError(t, f.Broker.WithSecret(testkit.Context(t), a, func(s core.Secret) error { called = !s.Empty(); return nil }))
	if !called {
		t.Fatal("owner connection check omitted secret")
	}
}
