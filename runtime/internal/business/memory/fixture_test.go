package memory

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
// business Service uses real Markdown files; SQLite holds assistant/source fixtures.
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

func init() {
	newContractFixture = func(t *testing.T) contractFixture {
		db := fixtureDB(t)
		fixturePut(t, db, "business.assistants", "owner/assistant", struct {
			ID, OwnerID core.ID
			State       string
		}{"assistant", "owner", "active"})
		fixturePut(t, db, "im.messages", "owner/source", struct{ Text string }{"original conversation"})
		fixturePut(t, db, "agent.checkpoints", "owner/source", struct{ Context string }{"private checkpoint"})
		root := t.TempDir()
		fail := false
		files := NewFiles(FileOptions{JournalRoot: filepath.Join(root, "journal"), BeforeCommit: func() error {
			if fail {
				fail = false
				return errors.New("injected file commit failure")
			}
			return nil
		}, Roots: func(ctx context.Context, owner core.ID) ([]Root, error) {
			return []Root{{Scope: Global, Directory: filepath.Join(root, string(owner), "global")}, {Scope: Assistant, AssistantID: "assistant", Directory: filepath.Join(root, string(owner), "work")}}, nil
		}})
		return contractFixture{Service: NewService(db, Options{Files: files, Authorizer: fixtureAuthorizer{}, CheckAssistant: fixtureCheckAssistant(db)}), Repository: files, FailCommitOnce: func() { fail = true }, Observe: func() (int, int, int) {
			return fixtureCount(t, db, "business.tasks"), fixtureCount(t, db, "policy.grants"), fixtureCount(t, db, "agent.events")
		}, SourceIntact: func() bool {
			return fixtureCount(t, db, "im.messages") == 1 && fixtureCount(t, db, "agent.checkpoints") == 1
		}}
	}
}

func TestTDDMissingAuthorizerFailsClosed(t *testing.T) {
	db := fixtureDB(t)
	service := NewService(db, Options{})
	_, err := service.List(testkit.Context(t), owner(), Query{})
	wantCode(t, err, core.Unavailable)
}
