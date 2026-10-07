package resource

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

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

func init() {
	newContractFixture = func(t *testing.T) contractFixture {
		db := fixtureDB(t)
		offline := false
		binding := Binding{Endpoint: "https://provider.example.test", RootPath: "/fixture/documents", CredentialID: "credential-secret-ref", AllowedOrigins: []string{"https://provider.example.test"}}
		provider := Provider{Kind: Storage, Validate: func(ctx context.Context, b Binding) ([]string, error) {
			if b.RootPath != binding.RootPath || b.Endpoint != binding.Endpoint {
				return nil, invalid("binding")
			}
			return []string{"read", "write"}, nil
		}, Check: func(context.Context, Binding) (Status, error) {
			state := Available
			if offline {
				state = Offline
			}
			return Status{State: state}, nil
		}}
		return contractFixture{Service: NewService(db, Options{Authorizer: fixtureAuthorizer{}, Providers: map[string]Provider{"fixture-storage": provider}}), Repository: NewRepository(db), SupportedProvider: "fixture-storage", SafeBinding: binding, SetProviderOffline: func(v bool) { offline = v }}
	}
}

func TestTDDMissingAuthorizerFailsClosed(t *testing.T) {
	db := fixtureDB(t)
	service := NewService(db, Options{})
	_, err := service.List(testkit.Context(t), owner(), Query{})
	wantCode(t, err, core.Unavailable)
}
