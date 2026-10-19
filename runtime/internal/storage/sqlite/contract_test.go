package sqlite_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/storage/sqlite"
	"molr.ai/runtime/tests/testkit"
)

// The factory opens a real, initially unmigrated temporary SQLite database.
// Reopen returns a fresh connection after Close. Fault hooks act on underlying
// commit/migration dependencies, not on the return values being asserted.
// Restore uses a separate offline restorer for this same test database.
type contractFixture struct {
	DB                sqlite.Database
	Restorer          sqlite.Restorer
	Reopen            func() sqlite.Database
	FailNextCommit    func(error)
	FailNextMigration func(error)
	SetFutureSchema   func()
}

var newContractFixture func(*testing.T) contractFixture

func fixture(t *testing.T) contractFixture {
	t.Helper()
	return testkit.RequireFactory(t, "storage/sqlite", newContractFixture)
}
func initialized(t *testing.T) (contractFixture, context.Context) {
	t.Helper()
	f := fixture(t)
	ctx := testkit.Context(t)
	testkit.RequireNoError(t, f.DB.Migrate(ctx))
	testkit.RequireNoError(t, f.DB.Store().Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return auth.NewRepository(tx.Store()).InsertAccount(ctx, auth.StoredAccount{Account: auth.Account{ID: "owner", Username: "owner", CreatedAt: time.Now()}, PasswordHash: core.NewSecret("opaque-password-hash"), Version: 1})
	}))
	return f, ctx
}
func record(key string) todo.Todo {
	return todo.Todo{ID: core.ID(key), OwnerID: "owner", Title: "Stored test data", Version: 1}
}
func stored(t *testing.T, db sqlite.Database, ctx context.Context, key string) (todo.Todo, error) {
	t.Helper()
	return todo.NewRepository(db.Store()).Get(ctx, "owner", core.ID(key))
}

func TestTDDSQLiteMigrateSchemaCheckAndClose(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	testkit.RequireNoError(t, f.DB.Migrate(ctx))
	first, err := f.DB.Schema(ctx)
	testkit.RequireNoError(t, err)
	if first.Version == 0 || first.CheckpointVersion == 0 {
		t.Fatal("migration left unspecified schema", first)
	}
	testkit.RequireNoError(t, f.DB.Migrate(ctx))
	second, err := f.DB.Schema(ctx)
	testkit.RequireNoError(t, err)
	if first != second {
		t.Fatal("repeated migration changed current version")
	}
	testkit.RequireNoError(t, f.DB.Check(ctx))
	testkit.RequireNoError(t, f.DB.Close())
	called := false
	err = f.DB.Store().Write(ctx, func(context.Context, persistence.Tx) error { called = true; return nil })
	if err == nil || called {
		t.Fatal("closed database accepted new transaction")
	}
}

func TestTDDSQLiteMigrationFailureDoesNotAdvanceVersion(t *testing.T) {
	f := fixture(t)
	ctx := testkit.Context(t)
	injected := errors.New("migration dependency failed")
	before, err := f.DB.Schema(ctx)
	testkit.RequireNoError(t, err)
	f.FailNextMigration(injected)
	if err = f.DB.Migrate(ctx); err == nil {
		t.Fatal("failed migration reported success")
	}
	after, err := f.DB.Schema(ctx)
	testkit.RequireNoError(t, err)
	if before != after {
		t.Fatal("failed migration advanced schema")
	}
	testkit.RequireNoError(t, f.DB.Migrate(ctx))
	f.SetFutureSchema()
	testkit.RequireCode(t, f.DB.Migrate(ctx), core.FailedPrecondition)
}

func TestTDDSQLiteCommitPersistsAcrossReopen(t *testing.T) {
	f, ctx := initialized(t)
	want := record("persist")
	testkit.RequireNoError(t, f.DB.Store().Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return todo.NewRepository(tx.Store()).Insert(ctx, want)
	}))
	testkit.RequireNoError(t, f.DB.Close())
	db := f.Reopen()
	t.Cleanup(func() { _ = db.Close() })
	got, err := stored(t, db, ctx, string(want.ID))
	testkit.RequireNoError(t, err)
	if got.OwnerID != want.OwnerID || got.Title != want.Title {
		t.Fatal("commit did not persist", got)
	}
}

func TestTDDSQLiteCallbackAndCommitErrorsRollBack(t *testing.T) {
	for _, phase := range []string{"callback", "commit"} {
		t.Run(phase, func(t *testing.T) {
			f, ctx := initialized(t)
			injected := errors.New("transaction failure")
			if phase == "commit" {
				f.FailNextCommit(injected)
			}
			err := f.DB.Store().Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				if err := todo.NewRepository(tx.Store()).Insert(ctx, record("rollback")); err != nil {
					return err
				}
				if err := todo.NewRepository(tx.Store()).Insert(ctx, todo.Todo{ID: "rollback-todo", OwnerID: "owner", Title: "must roll back", Version: 1}); err != nil {
					return err
				}
				if phase == "callback" {
					return injected
				}
				return nil
			})
			if err == nil {
				t.Fatal("failed transaction reported success")
			}
			if phase == "callback" && !errors.Is(err, injected) {
				t.Fatal("callback error lost", err)
			}
			_, err = stored(t, f.DB, ctx, "rollback")
			testkit.RequireCode(t, err, core.NotFound)
			testkit.RequireNoError(t, f.DB.Store().Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				_, err := todo.NewRepository(tx.Store()).Get(ctx, "owner", "rollback-todo")
				testkit.RequireCode(t, err, core.NotFound)
				return nil
			}))
		})
	}
}

func TestTDDSQLiteCanceledTransactionsDoNotCommit(t *testing.T) {
	f, ctx := initialized(t)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	called := false
	err := f.DB.Store().Write(canceled, func(context.Context, persistence.Tx) error { called = true; return nil })
	testkit.RequireCode(t, err, core.Canceled)
	if called {
		t.Fatal("callback ran after cancellation")
	}
	during, cancelDuring := context.WithCancel(ctx)
	defer cancelDuring()
	err = f.DB.Store().Write(during, func(ctx context.Context, tx persistence.Tx) error {
		if err := todo.NewRepository(tx.Store()).Insert(ctx, record("cancel-during")); err != nil {
			return err
		}
		cancelDuring()
		return nil
	})
	testkit.RequireCode(t, err, core.Canceled)
	_, err = stored(t, f.DB, ctx, "cancel-during")
	testkit.RequireCode(t, err, core.NotFound)
}

func TestTDDSQLiteExpiredTransactionHandleCannotWrite(t *testing.T) {
	f, ctx := initialized(t)
	var escaped persistence.Tx
	testkit.RequireNoError(t, f.DB.Store().Write(ctx, func(ctx context.Context, tx persistence.Tx) error { escaped = tx; return nil }))
	if err := escaped.Put(ctx, "business.todos", "owner/escaped", record("escaped")); err == nil {
		t.Fatal("repository escaped transaction lifetime")
	}
	_, err := stored(t, f.DB, ctx, "escaped")
	testkit.RequireCode(t, err, core.NotFound)
}

func TestTDDSQLiteMessageOutboxAndVisibleEventCommitTogether(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[commit], func(t *testing.T) {
			f, ctx := initialized(t)
			now := time.Now()
			testkit.RequireNoError(t, f.DB.Store().Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				return conversation.NewRepository(tx.Store()).Insert(ctx, conversation.Conversation{ID: "chat", OwnerID: "owner", PeerID: "assistant", Version: 1})
			}))
			abort := errors.New("abort")
			err := f.DB.Store().Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				msg, err := conversation.NewRepository(tx.Store()).Append(ctx, "owner", conversation.Message{ID: "message", ConversationID: "chat", Sender: conversation.User, SenderID: "owner", Content: conversation.Content{Kind: conversation.Text, Text: "hello"}, CreatedAt: now})
				if err != nil {
					return err
				}
				if err := conversation.NewOutbox(tx.Store()).Append(ctx, conversation.MessageCreated{ID: "im-event", OwnerID: "owner", ConversationID: "chat", MessageID: msg.ID, Sender: conversation.User, SenderID: "owner", At: now}); err != nil {
					return err
				}
				if err := realtime.NewStore(tx.Store()).Append(ctx, realtime.Visible{ID: "visible", OwnerID: "owner", Topic: "message.created", Object: core.Object{Kind: "message", ID: msg.ID}, CreatedAt: now}); err != nil {
					return err
				}
				if err := todo.NewRepository(tx.Store()).Insert(ctx, record("message-send")); err != nil {
					return err
				}
				if !commit {
					return abort
				}
				return nil
			})
			if commit {
				testkit.RequireNoError(t, err)
			} else if !errors.Is(err, abort) {
				t.Fatal(err)
			}
			testkit.RequireNoError(t, f.DB.Store().Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
				messages, err := conversation.NewRepository(tx.Store()).Messages(ctx, "owner", conversation.MessageQuery{ConversationID: "chat"})
				if err != nil {
					return err
				}
				outbox, err := conversation.NewOutbox(tx.Store()).Pending(ctx, 10)
				if err != nil {
					return err
				}
				visible, err := realtime.NewStore(tx.Store()).Replay(ctx, "owner", realtime.Query{})
				if err != nil {
					return err
				}
				want := 0
				if commit {
					want = 1
				}
				if len(messages.Items) != want || len(outbox) != want || len(visible.Items) != want {
					t.Fatalf("partial transaction %d/%d/%d, want %d", len(messages.Items), len(outbox), len(visible.Items), want)
				}
				return nil
			}))
		})
	}
}

func TestTDDSQLiteBackupRestoresDataOfflineAndRevokesAuthentication(t *testing.T) {
	f, ctx := initialized(t)
	now := time.Now()
	testkit.RequireNoError(t, f.DB.Store().Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := todo.NewRepository(tx.Store()).Insert(ctx, record("before-backup")); err != nil {
			return err
		}
		if err := auth.NewRepository(tx.Store()).SaveSession(ctx, auth.StoredSession{Session: auth.Session{ID: "session", AccountID: "owner", CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}, TokenDigest: "session-digest", CSRFDigest: "csrf-digest"}); err != nil {
			return err
		}
		return auth.NewRepository(tx.Store()).SaveToken(ctx, auth.StoredToken{Token: auth.Token{ID: "token", Name: "cli", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, AccountID: "owner", Digest: "token-digest"})
	}))
	backup, err := f.DB.Backup(ctx)
	testkit.RequireNoError(t, err)
	schema, err := f.DB.Schema(ctx)
	testkit.RequireNoError(t, err)
	if backup.ID == "" || backup.Schema != schema || backup.CreatedAt.IsZero() {
		t.Fatal("invalid snapshot metadata", backup)
	}
	testkit.RequireCode(t, f.Restorer.Restore(ctx, backup), core.FailedPrecondition)
	testkit.RequireNoError(t, f.DB.Store().Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return todo.NewRepository(tx.Store()).Insert(ctx, record("after-backup"))
	}))
	testkit.RequireNoError(t, f.DB.Close())
	testkit.RequireNoError(t, f.Restorer.Restore(ctx, backup))
	db := f.Reopen()
	t.Cleanup(func() { _ = db.Close() })
	_, err = stored(t, db, ctx, "before-backup")
	testkit.RequireNoError(t, err)
	_, err = stored(t, db, ctx, "after-backup")
	testkit.RequireCode(t, err, core.NotFound)
	testkit.RequireNoError(t, db.Store().Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		session, err := auth.NewRepository(tx.Store()).FindSession(ctx, "session-digest")
		if err == nil && session.RevokedAt == nil {
			t.Fatal("backup resurrected active session")
		}
		if err != nil && core.CodeOf(err) != core.NotFound {
			return err
		}
		token, err := auth.NewRepository(tx.Store()).FindToken(ctx, "token-digest")
		if err == nil && token.RevokedAt == nil {
			t.Fatal("backup resurrected active access token")
		}
		if err != nil && core.CodeOf(err) != core.NotFound {
			return err
		}
		return nil
	}))
}
