package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/im/realtime"
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
	testkit.RequireNoError(t, f.DB.Within(ctx, func(r sqlite.Repositories) error {
		return r.Auth().InsertAccount(ctx, auth.StoredAccount{Account: auth.Account{ID: "owner", Username: "owner", CreatedAt: time.Now()}, PasswordHash: core.NewSecret("opaque-password-hash"), Version: 1})
	}))
	return f, ctx
}
func receipt(key string) sqlite.Receipt {
	return sqlite.Receipt{OwnerID: "owner", Action: "test.write", Key: key, InputDigest: "digest-one", Result: json.RawMessage(`{"id":"result"}`), CreatedAt: time.Now()}
}
func stored(t *testing.T, db sqlite.Database, ctx context.Context, key string) (sqlite.Receipt, error) {
	t.Helper()
	var got sqlite.Receipt
	err := db.Within(ctx, func(r sqlite.Repositories) error {
		var e error
		got, e = r.Idempotency().Get(ctx, "owner", "test.write", key)
		return e
	})
	return got, err
}
func nonNil(t *testing.T, name string, value any) {
	t.Helper()
	if value == nil {
		t.Fatalf("%s repository is nil", name)
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		if v.IsNil() {
			t.Fatalf("%s repository is typed nil", name)
		}
	}
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
	err = f.DB.Within(ctx, func(sqlite.Repositories) error { called = true; return nil })
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

func TestTDDSQLiteRepositoriesAreAvailableInTransaction(t *testing.T) {
	f, ctx := initialized(t)
	testkit.RequireNoError(t, f.DB.Within(ctx, func(r sqlite.Repositories) error {
		for name, repo := range map[string]any{
			"Auth": r.Auth(), "Audit": r.Audit(), "Assistants": r.Assistants(), "Conversations": r.Conversations(), "IMOutbox": r.IMOutbox(),
			"Tasks": r.Tasks(), "Todos": r.Todos(), "Schedules": r.Schedules(), "Resources": r.Resources(), "Settings": r.Settings(),
			"Credentials": r.Credentials(), "Policy": r.Policy(), "Operations": r.Operations(), "ModelReceipts": r.ModelReceipts(), "Checkpoints": r.Checkpoints(),
			"AssistantEvents": r.AssistantEvents(), "Realtime": r.Realtime(), "Queue": r.Queue(), "Files": r.Files(), "Idempotency": r.Idempotency(),
		} {
			nonNil(t, name, repo)
		}
		return nil
	}))
}

func TestTDDSQLiteCommitPersistsAcrossReopen(t *testing.T) {
	f, ctx := initialized(t)
	want := receipt("persist")
	testkit.RequireNoError(t, f.DB.Within(ctx, func(r sqlite.Repositories) error { return r.Idempotency().Insert(ctx, want) }))
	testkit.RequireNoError(t, f.DB.Close())
	db := f.Reopen()
	t.Cleanup(func() { _ = db.Close() })
	got, err := stored(t, db, ctx, want.Key)
	testkit.RequireNoError(t, err)
	if got.OwnerID != want.OwnerID || got.InputDigest != want.InputDigest || string(got.Result) != string(want.Result) {
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
			err := f.DB.Within(ctx, func(r sqlite.Repositories) error {
				if err := r.Idempotency().Insert(ctx, receipt("rollback")); err != nil {
					return err
				}
				if err := r.Todos().Insert(ctx, todo.Todo{ID: "rollback-todo", OwnerID: "owner", Title: "must roll back", Version: 1}); err != nil {
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
			testkit.RequireNoError(t, f.DB.Within(ctx, func(r sqlite.Repositories) error {
				_, err := r.Todos().Get(ctx, "owner", "rollback-todo")
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
	err := f.DB.Within(canceled, func(sqlite.Repositories) error { called = true; return nil })
	testkit.RequireCode(t, err, core.Canceled)
	if called {
		t.Fatal("callback ran after cancellation")
	}
	during, cancelDuring := context.WithCancel(ctx)
	defer cancelDuring()
	err = f.DB.Within(during, func(r sqlite.Repositories) error {
		if err := r.Idempotency().Insert(during, receipt("cancel-during")); err != nil {
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
	var escaped sqlite.Idempotency
	testkit.RequireNoError(t, f.DB.Within(ctx, func(r sqlite.Repositories) error { escaped = r.Idempotency(); return nil }))
	if err := escaped.Insert(ctx, receipt("escaped")); err == nil {
		t.Fatal("repository escaped transaction lifetime")
	}
	_, err := stored(t, f.DB, ctx, "escaped")
	testkit.RequireCode(t, err, core.NotFound)
}

func TestTDDSQLiteIdempotencyIsScopedAndPayloadBound(t *testing.T) {
	f, ctx := initialized(t)
	original := receipt("key")
	testkit.RequireNoError(t, f.DB.Within(ctx, func(r sqlite.Repositories) error { return r.Idempotency().Insert(ctx, original) }))
	testkit.RequireNoError(t, f.DB.Within(ctx, func(r sqlite.Repositories) error {
		got, err := r.Idempotency().Get(ctx, "owner", "test.write", "key")
		if err != nil {
			return err
		}
		if got.InputDigest != original.InputDigest {
			t.Fatal("payload digest lost")
		}
		for _, parts := range [][2]string{{"other", "test.write"}, {"owner", "other.action"}} {
			_, err := r.Idempotency().Get(ctx, core.ID(parts[0]), parts[1], "key")
			testkit.RequireCode(t, err, core.NotFound)
		}
		return nil
	}))
	changed := original
	changed.InputDigest = "different"
	err := f.DB.Within(ctx, func(r sqlite.Repositories) error { return r.Idempotency().Insert(ctx, changed) })
	testkit.RequireCode(t, err, core.Conflict)
	got, err := stored(t, f.DB, ctx, "key")
	testkit.RequireNoError(t, err)
	if got.InputDigest != original.InputDigest {
		t.Fatal("conflict overwrote original receipt")
	}
}

func TestTDDSQLiteConcurrentIdempotencyKeyHasOneWinner(t *testing.T) {
	f, ctx := initialized(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, digest := range []string{"first", "second"} {
		go func(d string) {
			<-start
			r := receipt("concurrent")
			r.InputDigest = d
			results <- f.DB.Within(ctx, func(tx sqlite.Repositories) error { return tx.Idempotency().Insert(ctx, r) })
		}(digest)
	}
	close(start)
	success, conflict := 0, 0
	for range 2 {
		var err error
		select {
		case err = <-results:
		case <-ctx.Done():
			t.Fatal("concurrent transactions did not finish before watchdog")
		}
		if err == nil {
			success++
		} else if core.CodeOf(err) == core.Conflict {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}

func TestTDDSQLiteMessageOutboxAndVisibleEventCommitTogether(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[commit], func(t *testing.T) {
			f, ctx := initialized(t)
			now := time.Now()
			testkit.RequireNoError(t, f.DB.Within(ctx, func(tx sqlite.Repositories) error {
				return tx.Conversations().Insert(ctx, conversation.Conversation{ID: "chat", OwnerID: "owner", PeerID: "assistant", Version: 1})
			}))
			abort := errors.New("abort")
			err := f.DB.Within(ctx, func(tx sqlite.Repositories) error {
				msg, err := tx.Conversations().Append(ctx, "owner", conversation.Message{ID: "message", ConversationID: "chat", Sender: conversation.User, SenderID: "owner", Content: conversation.Content{Kind: conversation.Text, Text: "hello"}, CreatedAt: now})
				if err != nil {
					return err
				}
				if err := tx.IMOutbox().Append(ctx, conversation.MessageCreated{ID: "im-event", OwnerID: "owner", ConversationID: "chat", MessageID: msg.ID, Sender: conversation.User, SenderID: "owner", At: now}); err != nil {
					return err
				}
				if err := tx.Realtime().Append(ctx, realtime.Visible{ID: "visible", OwnerID: "owner", Topic: "message.created", Object: core.Object{Kind: "message", ID: msg.ID}, CreatedAt: now}); err != nil {
					return err
				}
				if err := tx.Idempotency().Insert(ctx, receipt("message-send")); err != nil {
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
			testkit.RequireNoError(t, f.DB.Within(ctx, func(tx sqlite.Repositories) error {
				messages, err := tx.Conversations().Messages(ctx, "owner", conversation.MessageQuery{ConversationID: "chat"})
				if err != nil {
					return err
				}
				outbox, err := tx.IMOutbox().Pending(ctx, 10)
				if err != nil {
					return err
				}
				visible, err := tx.Realtime().Replay(ctx, "owner", realtime.Query{})
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
	testkit.RequireNoError(t, f.DB.Within(ctx, func(tx sqlite.Repositories) error {
		if err := tx.Idempotency().Insert(ctx, receipt("before-backup")); err != nil {
			return err
		}
		if err := tx.Auth().SaveSession(ctx, auth.StoredSession{Session: auth.Session{ID: "session", AccountID: "owner", CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}, TokenDigest: "session-digest", CSRFDigest: "csrf-digest"}); err != nil {
			return err
		}
		return tx.Auth().SaveToken(ctx, auth.StoredToken{Token: auth.Token{ID: "token", Name: "cli", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, AccountID: "owner", Digest: "token-digest"})
	}))
	backup, err := f.DB.Backup(ctx)
	testkit.RequireNoError(t, err)
	schema, err := f.DB.Schema(ctx)
	testkit.RequireNoError(t, err)
	if backup.ID == "" || backup.Schema != schema || backup.CreatedAt.IsZero() {
		t.Fatal("invalid snapshot metadata", backup)
	}
	testkit.RequireCode(t, f.Restorer.Restore(ctx, backup), core.FailedPrecondition)
	testkit.RequireNoError(t, f.DB.Within(ctx, func(tx sqlite.Repositories) error { return tx.Idempotency().Insert(ctx, receipt("after-backup")) }))
	testkit.RequireNoError(t, f.DB.Close())
	testkit.RequireNoError(t, f.Restorer.Restore(ctx, backup))
	db := f.Reopen()
	t.Cleanup(func() { _ = db.Close() })
	_, err = stored(t, db, ctx, "before-backup")
	testkit.RequireNoError(t, err)
	_, err = stored(t, db, ctx, "after-backup")
	testkit.RequireCode(t, err, core.NotFound)
	testkit.RequireNoError(t, db.Within(ctx, func(tx sqlite.Repositories) error {
		session, err := tx.Auth().FindSession(ctx, "session-digest")
		if err == nil && session.RevokedAt == nil {
			t.Fatal("backup resurrected active session")
		}
		if err != nil && core.CodeOf(err) != core.NotFound {
			return err
		}
		token, err := tx.Auth().FindToken(ctx, "token-digest")
		if err == nil && token.RevokedAt == nil {
			t.Fatal("backup resurrected active access token")
		}
		if err != nil && core.CodeOf(err) != core.NotFound {
			return err
		}
		return nil
	}))
}
