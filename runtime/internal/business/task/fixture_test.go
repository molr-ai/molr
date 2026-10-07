package task

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
		fixturePut(t, db, "business.assistants", "owner/assistant", struct {
			ID, OwnerID core.ID
			State       string
		}{"assistant", "owner", "active"})
		for _, id := range []core.ID{"artifact", "evidence"} {
			fixturePut(t, db, "fixture.artifacts", persistence.Key("owner", id), struct{ Committed bool }{true})
		}
		options := Options{Authorizer: fixtureAuthorizer{}, CheckAssistant: fixtureCheckAssistant(db), Emit: func(ctx context.Context, p core.Principal, v Task, kind string) error {
			return db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				return tx.Put(ctx, "agent.events", persistence.Key(v.OwnerID, core.ID(string(v.ID)+"-"+kind)), struct {
					TaskID core.ID
					Kind   string
				}{v.ID, kind})
			})
		}, ValidateExecution: func(ctx context.Context, e core.Execution) error {
			stored, err := persistence.Load[core.Execution](ctx, db, "fixture.executions", string(e.Principal.ExecutionID))
			if err != nil {
				return err
			}
			if stored != e {
				return forbidden()
			}
			return nil
		}, VerifyEvidence: func(ctx context.Context, e core.Execution, d Delivery) error {
			ids := append([]core.ID{}, d.ArtifactIDs...)
			for _, c := range d.Checks {
				ids = append(ids, c.EvidenceIDs...)
			}
			for _, id := range ids {
				var record struct{ Committed bool }
				err := db.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
					return tx.Get(ctx, "fixture.artifacts", persistence.Key(e.Principal.OwnerID, id), &record)
				})
				if err != nil {
					return err
				}
				if !record.Committed {
					return core.NewError(core.FailedPrecondition, "artifact not committed")
				}
			}
			return nil
		}}
		repo := NewRepository(db)
		return contractFixture{Service: NewService(db, options), Execution: NewExecution(db, options), Repository: repo, ArtifactID: "artifact", EvidenceID: "evidence", FailCommitOnce: fixtureFailure(db), Observe: func() (int, int, int) {
			return fixtureCount(t, db, "business.tasks"), fixtureCount(t, db, "business.revisions"), fixtureCount(t, db, "agent.events")
		}, ExecutionFor: func(id core.ID) core.Execution {
			execution := core.Execution{Principal: agent(), AssistantID: "assistant", TaskID: id, OperationID: "operation"}
			fixturePut(t, db, "fixture.executions", string(execution.Principal.ExecutionID), execution)
			return execution
		}, RecordSpent: func(id core.ID, spent core.Money) {
			ctx := testkit.Context(t)
			v, err := repo.Get(ctx, "owner", id)
			if err != nil {
				t.Fatal(err)
			}
			old := v.Version
			v.Version++
			v.Spent = spent
			if err = repo.Save(ctx, v, old); err != nil {
				t.Fatal(err)
			}
		}}
	}
}

func TestTDDMissingAuthorizerFailsClosed(t *testing.T) {
	db := fixtureDB(t)
	service := NewService(db, Options{})
	_, err := service.List(testkit.Context(t), owner(), Query{})
	wantCode(t, err, core.Unavailable)
}
