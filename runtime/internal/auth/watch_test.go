package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/tests/testkit"
)

func TestPrincipalWatchCommitRollbackAndOwnerScope(t *testing.T) {
	f := fixture(t)
	p, _ := initialized(t, f)
	m := f.Service.(*Manager)
	w, err := m.WatchPrincipal(p)
	testkit.RequireNoError(t, err)
	defer w.Close()
	other, err := m.WatchPrincipal(core.Principal{OwnerID: "other", SubjectID: "other", Kind: core.ActorOwner, SessionID: "session"})
	testkit.RequireNoError(t, err)
	defer other.Close()
	ctx := t.Context()
	rollback := errors.New("rollback")
	err = m.store.Write(ctx, func(ctx context.Context, _ persistence.Tx) error {
		if err := m.repo.RevokeSession(ctx, p.OwnerID, p.SessionID, f.Now); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	select {
	case <-w.Changes():
		t.Fatal("rollback notified")
	default:
	}
	_, err = w.Check(ctx)
	testkit.RequireNoError(t, err)
	db := m.store.(*sqlitestore.Store)
	db.FailNextCommit(rollback)
	if err = m.repo.RevokeSession(ctx, p.OwnerID, p.SessionID, f.Now); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	select {
	case <-w.Changes():
		t.Fatal("failed commit notified")
	default:
	}
	testkit.RequireNoError(t, m.repo.RevokeSession(ctx, p.OwnerID, p.SessionID, f.Now))
	select {
	case <-w.Changes():
	case <-time.After(time.Second):
		t.Fatal("commit did not notify")
	}
	select {
	case <-other.Changes():
		t.Fatal("another owner notified")
	default:
	}
	_, err = w.Check(ctx)
	testkit.RequireCode(t, err, core.Unauthenticated)
}

func TestPrincipalWatchExpiryAndActivity(t *testing.T) {
	f := fixture(t)
	p, session := initialized(t, f)
	m := f.Service.(*Manager)
	m.options.SessionIdle = time.Minute
	w, err := m.WatchPrincipal(p)
	testkit.RequireNoError(t, err)
	defer w.Close()
	remaining, err := w.Check(t.Context())
	testkit.RequireNoError(t, err)
	if remaining != time.Minute {
		t.Fatal(remaining)
	}
	f.Advance(40 * time.Second)
	_, err = m.Authenticate(t.Context(), Authentication{Session: session.Token})
	testkit.RequireNoError(t, err)
	select {
	case <-w.Changes():
		t.Fatal("activity should not wake every stream")
	default:
	}
	f.Advance(20 * time.Second)
	remaining, err = w.Check(t.Context())
	testkit.RequireNoError(t, err)
	if remaining != 40*time.Second {
		t.Fatal("renewed idle expiry", remaining)
	}
	stored, err := m.repo.FindSession(t.Context(), digest(session.Token.Reveal()))
	testkit.RequireNoError(t, err)
	stored.Session.ExpiresAt = f.Now.Add(70 * time.Second)
	testkit.RequireNoError(t, m.repo.SaveSession(t.Context(), stored))
	remaining, err = w.Check(t.Context())
	testkit.RequireNoError(t, err)
	if remaining != 10*time.Second {
		t.Fatal("absolute expiry", remaining)
	}
	f.Advance(10 * time.Second)
	_, err = w.Check(t.Context())
	testkit.RequireCode(t, err, core.Unauthenticated)
}

func TestPrincipalWatchAccountTokenAndRevokeAll(t *testing.T) {
	for _, action := range []string{"account", "token", "all"} {
		t.Run(action, func(t *testing.T) {
			f := fixture(t)
			p, _ := initialized(t, f)
			m := f.Service.(*Manager)
			token := StoredToken{AccountID: p.OwnerID, Token: Token{ID: "token", Scopes: []string{"read"}, ExpiresAt: f.Now.Add(time.Minute)}, Digest: "synthetic"}
			testkit.RequireNoError(t, m.repo.SaveToken(t.Context(), token))
			principal := p
			if action == "token" {
				principal.SessionID = ""
				principal.TokenID = token.Token.ID
			}
			w, err := m.WatchPrincipal(principal)
			testkit.RequireNoError(t, err)
			defer w.Close()
			if action == "token" {
				remaining, err := w.Check(t.Context())
				testkit.RequireNoError(t, err)
				if remaining != time.Minute {
					t.Fatal(remaining)
				}
				testkit.RequireNoError(t, m.repo.RevokeToken(t.Context(), p.OwnerID, token.Token.ID, f.Now))
			} else if action == "all" {
				testkit.RequireNoError(t, m.repo.RevokeAll(t.Context(), p.OwnerID, f.Now))
			} else {
				a, err := m.repo.AccountByID(t.Context(), p.OwnerID)
				testkit.RequireNoError(t, err)
				a.Account.Disabled = true
				testkit.RequireNoError(t, m.repo.SaveAccount(t.Context(), a, a.Version))
			}
			select {
			case <-w.Changes():
			case <-time.After(time.Second):
				t.Fatal("missing change")
			}
			_, err = w.Check(t.Context())
			testkit.RequireCode(t, err, core.Unauthenticated)
		})
	}
}
