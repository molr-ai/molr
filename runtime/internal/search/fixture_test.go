package search

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
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

type searchFixtureAuthorizer struct{ store persistence.Store }

func (a searchFixtureAuthorizer) Require(ctx context.Context, p core.Principal, q policy.CheckRequest) error {
	if err := (fixtureAuthorizer{}).Require(ctx, p, q); err != nil {
		return err
	}
	if q.Target.ID == "" {
		return nil
	}
	var denied bool
	err := a.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Get(ctx, "fixture.denied", persistence.Key(p.OwnerID, q.Target.ID), &denied)
	})
	if err != nil && !persistence.Missing(err) {
		return err
	}
	if denied {
		return forbidden()
	}
	return nil
}
func init() {
	newContractFixture = func(t *testing.T) contractFixture {
		db := fixtureDB(t)
		ctx := testkit.Context(t)
		ids := map[Source]core.ID{Assistants: "assistant", Resources: "resource", Tasks: "task", Memories: "memory", Messages: "message"}
		fixturePut(t, db, "business.assistants", "owner/assistant", assistant.Assistant{ID: "assistant", OwnerID: "owner", Name: "Budget assistant", State: assistant.Active, ConversationID: "conversation", Version: 1})
		resourceRow := resource.Resource{ID: "resource", OwnerID: "owner", Kind: resource.Storage, Name: "Budget resource", State: resource.Available, Version: 1}
		fixturePut(t, db, "business.resources", "owner/resource", resourceRow)
		fixturePut(t, db, "business.tasks", "owner/task", task.Task{ID: "task", OwnerID: "owner", AssistantID: "assistant", Goal: "Budget review", State: task.Pending, Version: 1})
		root := t.TempDir()
		memoryFiles := memory.NewFiles(memory.FileOptions{JournalRoot: filepath.Join(root, "journal"), Roots: func(ctx context.Context, owner core.ID, query memory.RootQuery) ([]memory.Root, error) {
			return []memory.Root{{Scope: memory.Assistant, AssistantID: "assistant", Directory: filepath.Join(root, string(owner), "work")}}, nil
		}})
		item, err := memoryFiles.Put(ctx, memory.Memory{OwnerID: "owner", Text: "Budget preference", Ref: memory.Ref{Path: ".agents/memory/preference.md", AssistantID: "assistant", Scope: memory.Assistant}}, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		ids[Memories] = core.ID(item.Path)
		memories := memory.NewService(db, memory.Options{Files: memoryFiles, Authorizer: searchFixtureAuthorizer{store: db}})

		im := conversation.NewRepository(db)
		for _, row := range []struct {
			owner, conversation, message core.ID
			archive                      bool
		}{{"owner", "conversation", "message", false}, {"owner", "archive", "archived-message", true}, {"other", "other-conversation", "secret-message", false}} {
			if err := im.Insert(ctx, conversation.Conversation{ID: row.conversation, OwnerID: row.owner, PeerID: "assistant", Version: 1}); err != nil {
				t.Fatal(err)
			}
			if _, err := im.Append(ctx, row.owner, conversation.Message{ID: row.message, ConversationID: row.conversation, Sender: conversation.User, SenderID: row.owner, Content: conversation.Content{Kind: conversation.Text, Text: "Budget discussion"}, CreatedAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
			if row.archive {
				if err := im.SetArchived(ctx, row.owner, row.conversation, true, 1); err != nil {
					t.Fatal(err)
				}
			}
		}
		options := Options{Memories: memories, Authorizer: searchFixtureAuthorizer{store: db}}
		indexer := NewIndexer(db, options)
		if _, err := indexer.Rebuild(ctx, owner()); err != nil {
			t.Fatal(err)
		}
		return contractFixture{Service: NewService(db, options), Maintenance: indexer, Indexer: indexer, VisibleIDs: ids, SecretID: "secret-message", ArchivedID: "archived-message", SetAccess: func(id core.ID, allowed bool) {
			fixturePut(t, db, "fixture.denied", persistence.Key("owner", id), !allowed)
		}, SetSourceVersion: func(object core.Object, version core.Version, deleted bool) {
			if object.Kind != "resource" || object.ID != "resource" {
				t.Fatalf("unsupported fixture source %+v", object)
			}
			if deleted {
				if err := db.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
					return tx.Delete(ctx, "business.resources", "owner/resource")
				}); err != nil {
					t.Fatal(err)
				}
			} else {
				row := resourceRow
				row.Version = version
				fixturePut(t, db, "business.resources", "owner/resource", row)
			}
		}, DrainIndex: func(ctx context.Context) error {
			jobs, err := persistence.Scan[IndexStatus](ctx, db, statusBucket)
			if err != nil {
				return err
			}
			for _, job := range jobs {
				if job.State != "completed" {
					return core.NewError(core.FailedPrecondition, "index job not completed")
				}
			}
			return nil
		}}
	}
}

func TestTDDMissingAuthorizerFailsClosed(t *testing.T) {
	db := fixtureDB(t)
	service := NewService(db, Options{})
	_, err := service.Search(testkit.Context(t), owner(), Query{Text: "budget"})
	wantCode(t, err, core.Unavailable)
}
