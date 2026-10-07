package settings

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/tests/testkit"
)

// fixtureAuthorizer is the controlled authentication/policy dependency. The
// business Service and Repository below are real implementations over SQLite.
type fixtureAuthorizer struct{}

func (fixtureAuthorizer) Require(ctx context.Context, p core.Principal, q policy.CheckRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if p.OwnerID != "owner" && p.OwnerID != "other" {
		return core.NewError(core.Unauthenticated, "unknown test identity")
	}
	if p.Kind == core.ActorAssistant && q.Target.Kind == "shared_memory" && q.Action == policy.Write {
		return core.NewError(core.Forbidden, "shared write requires grant")
	}
	return nil
}
func fixtureDB(t *testing.T) *sqlitestore.Store {
	t.Helper()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}
func fixturePut(t *testing.T, db persistence.Store, bucket, key string, value any) {
	t.Helper()
	if err := db.Write(testkit.Context(t), func(ctx context.Context, tx persistence.Tx) error { return tx.Put(ctx, bucket, key, value) }); err != nil {
		t.Fatal(err)
	}
}
func fixtureCount(t *testing.T, db persistence.Store, bucket string) int {
	t.Helper()
	n := 0
	if err := db.Read(testkit.Context(t), func(ctx context.Context, tx persistence.Tx) error {
		rows, err := tx.Scan(ctx, bucket)
		n = len(rows)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return n
}
func fixtureCheckAssistant(db persistence.Store) func(context.Context, core.Principal, core.ID) error {
	return func(ctx context.Context, p core.Principal, id core.ID) error {
		var row struct {
			ID, OwnerID core.ID
			State       string
		}
		err := db.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return tx.Get(ctx, "business.assistants", persistence.Key(p.OwnerID, id), &row)
		})
		if err != nil {
			return err
		}
		if row.State != "active" {
			return core.NewError(core.FailedPrecondition, "assistant dismissed")
		}
		return nil
	}
}
func fixtureFailure(db *sqlitestore.Store) func() {
	return func() { db.FailNextCommit(errors.New("injected commit failure")) }
}

type fixtureVerification struct {
	Owner, Session core.ID
	Purpose        string
	Expires        time.Time
	Consumed       bool
}

func init() {
	newContractFixture = func(t *testing.T) contractFixture {
		db := fixtureDB(t)
		ctx := testkit.Context(t)
		now := time.Now().UTC()
		repo := NewRepository(db)
		initial := Settings{OwnerID: "owner", Language: "en", Timezone: "UTC", Currency: "USD", Theme: System, Version: 1}
		if err := repo.Save(ctx, initial, 0); err != nil {
			t.Fatal(err)
		}
		fixturePut(t, db, "business.assistants", "owner/active", struct{ Budget core.AssistantBudget }{core.AssistantBudget{Limit: core.Money{Currency: "USD", Micros: 2000000}, Period: "daily"}})
		owned := core.Object{Kind: "task", ID: "owned-memory"}
		foreign := core.Object{Kind: "task", ID: "foreign-memory"}
		fixturePut(t, db, "business.tasks", "owner/owned-memory", struct {
			OwnerID core.ID
			Text    string
		}{"owner", "Owned fact"})
		fixturePut(t, db, "business.tasks", "other/foreign-memory", struct {
			OwnerID core.ID
			Text    string
		}{"other", "Foreign fact"})
		options := Options{Authorizer: fixtureAuthorizer{}, Now: func() time.Time { return now }, CheckVerification: func(ctx context.Context, p core.Principal, v auth.Verification, purpose string) error {
			return db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				var proof fixtureVerification
				if err := tx.Get(ctx, "fixture.verifications", v.Token.Reveal(), &proof); err != nil {
					return forbidden()
				}
				if proof.Consumed || proof.Owner != p.OwnerID || proof.Session != p.SessionID || proof.Purpose != purpose || !proof.Expires.After(now) {
					return forbidden()
				}
				proof.Consumed = true
				return tx.Put(ctx, "fixture.verifications", v.Token.Reveal(), proof)
			})
		}, SaveExport: func(ctx context.Context, p core.Principal, b ExportBundle) (core.ID, error) {
			id := core.ID("file-" + string(b.ID))
			err := db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				return tx.Put(ctx, "fixture.export-files", persistence.Key(p.OwnerID, id), b)
			})
			return id, err
		}, CleanupObject: func(ctx context.Context, p core.Principal, o core.Object, apply bool) error {
			if o.Kind != "task" {
				return invalid("object_kind")
			}
			return db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				var row struct{ OwnerID core.ID }
				if err := tx.Get(ctx, "business.tasks", persistence.Key(p.OwnerID, o.ID), &row); err != nil {
					return err
				}
				if row.OwnerID != p.OwnerID {
					return forbidden()
				}
				if apply {
					return tx.Delete(ctx, "business.tasks", persistence.Key(p.OwnerID, o.ID))
				}
				return nil
			})
		}}
		return contractFixture{Service: NewService(db, options), Repository: repo, OwnedObject: owned, ForeignObject: foreign, Verify: func(p core.Principal, purpose string) auth.Verification {
			token := string(newID())
			fixturePut(t, db, "fixture.verifications", token, fixtureVerification{Owner: p.OwnerID, Session: p.SessionID, Purpose: purpose, Expires: now.Add(time.Hour)})
			return auth.Verification{Token: core.NewSecret(token)}
		}, ExpireVerification: func() { now = now.Add(2 * time.Hour) }, AssistantBudget: func() core.AssistantBudget {
			var row struct{ Budget core.AssistantBudget }
			if err := db.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
				return tx.Get(ctx, "business.assistants", "owner/active", &row)
			}); err != nil {
				t.Fatal(err)
			}
			return row.Budget
		}, Exists: func(o core.Object) bool {
			owner := core.ID("owner")
			if o.ID == foreign.ID {
				owner = "other"
			}
			var row struct{ OwnerID core.ID }
			err := db.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
				return tx.Get(ctx, "business.tasks", persistence.Key(owner, o.ID), &row)
			})
			if err != nil && !persistence.Missing(err) {
				t.Fatal(err)
			}
			return err == nil
		}}
	}
}

func TestTDDMissingAuthorizerFailsClosed(t *testing.T) {
	db := fixtureDB(t)
	service := NewService(db, Options{})
	_, err := service.Get(testkit.Context(t), owner())
	wantCode(t, err, core.Unavailable)
}
