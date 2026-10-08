package realtime

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/tests/testkit"
)

type fixtureAuthorizer struct{ store persistence.Store }

func (a fixtureAuthorizer) Require(ctx context.Context, p core.Principal, _ policy.CheckRequest) error {
	if err := p.RequireOwner(); err != nil {
		return err
	}
	live, err := persistence.Load[bool](ctx, a.store, "test.sessions", persistence.Key(p.OwnerID, p.SessionID))
	if err != nil && !persistence.Missing(err) {
		return err
	}
	if !live {
		return core.NewError(core.Unauthenticated, "session revoked")
	}
	return nil
}
func init() {
	newContractFixture = func(t *testing.T) contractFixture {
		t.Helper()
		ctx := testkit.Context(t)
		db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "realtime.db"))
		testkit.RequireNoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		owner := core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}
		other := core.Principal{OwnerID: "other", SubjectID: "other", Kind: core.ActorOwner, SessionID: "other-session"}
		testkit.RequireNoError(t, db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			for _, p := range []core.Principal{owner, other} {
				if err := tx.Put(ctx, "test.sessions", persistence.Key(p.OwnerID, p.SessionID), true); err != nil {
					return err
				}
			}
			return nil
		}))
		store := NewStore(db)
		return contractFixture{Store: store, Feed: NewFeed(store, Options{Authorizer: fixtureAuthorizer{db}, AuthorizationInterval: time.Millisecond}), Owner: owner, Other: other, Revoke: func(p core.Principal) {
			testkit.RequireNoError(t, db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				return tx.Put(ctx, "test.sessions", persistence.Key(p.OwnerID, p.SessionID), false)
			}))
		}}
	}
}
