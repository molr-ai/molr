package audit

import (
	"context"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
	"path/filepath"
	"testing"
	"time"
)

func init() {
	newContractFixture = func(t *testing.T) contractFixture {
		store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "audit.db"))
		testkit.RequireNoError(t, err)
		t.Cleanup(func() { _ = store.Close() })
		ctx := testkit.Context(t)
		now := time.Now().UTC()
		authentication, err := auth.NewService(store, auth.Options{MasterKey: make([]byte, 32), Origin: "https://molr.example", Now: func() time.Time { return now }})
		testkit.RequireNoError(t, err)
		_, err = authentication.Initialize(ctx, auth.InitializeRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!")})
		testkit.RequireNoError(t, err)
		login, err := authentication.Login(ctx, auth.LoginRequest{Username: "owner", Password: core.NewSecret("strong-initial-password-123!")})
		testkit.RequireNoError(t, err)
		owner, err := authentication.Authenticate(ctx, auth.Authentication{Session: login.Session.Token})
		testkit.RequireNoError(t, err)
		// Start each audit contract with an empty event history after real login setup.
		testkit.RequireNoError(t, store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			records, e := tx.Scan(ctx, "security.audit")
			if e != nil {
				return e
			}
			for _, r := range records {
				if e = tx.Delete(ctx, "security.audit", r.Key); e != nil {
					return e
				}
			}
			return nil
		}))
		service, err := NewService(store, Options{ValidatePrincipal: authentication.ValidatePrincipal})
		testkit.RequireNoError(t, err)
		return contractFixture{Service: service, Repository: NewRepository(store), Owner: owner, OtherOwner: core.Principal{OwnerID: "other", SubjectID: "other", Kind: core.ActorOwner, SessionID: "other-session"}, Assistant: core.Principal{OwnerID: owner.OwnerID, SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "execution"}, Now: now, FailWrites: func() { testkit.RequireNoError(t, store.Close()) }}
	}
}
func TestAuditConstructorRequiresLiveValidator(t *testing.T) {
	if _, err := NewService(nil, Options{ValidatePrincipal: func(context.Context, core.Principal) error { return nil }}); err == nil {
		t.Fatal("nil store accepted")
	}
}
