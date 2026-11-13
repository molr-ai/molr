package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func seedV2(t *testing.T, db *DB) {
	t.Helper()
	err := db.Store().Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		rows := []struct{ bucket, key, raw string }{
			{"system", "schema", `{"Version":2,"CheckpointVersion":1}`},
			{"business.todos", "owner/todo", `{"ID":"todo","OwnerID":"owner","Version":7,"Completed":true}`},
			{"business.plan-links", "owner/todo-todo", `{"OwnerID":"owner","Ref":{"Kind":"todo","ID":"todo"},"Paused":true,"Version":3,"Runs":[{"ID":"history"}]}`},
			{"business.schedules", "owner/disabled", `{"ID":"disabled","OwnerID":"owner","Version":4,"Enabled":false,"NextAt":null}`},
			{"business.schedules", "owner/active", `{"ID":"active","OwnerID":"owner","Version":2,"Enabled":true,"NextAt":"2030-01-01T00:00:00Z"}`},
			{"business.watches", "owner/watch", `{"ID":"watch","OwnerID":"owner","Version":1,"Paused":false,"State":"blocked","Recent":[{"Key":"event"}],"Checkpoint":{"cursor":9}}`},
			{"business.schedules", "owner/exhausted", `{"ID":"exhausted","OwnerID":"owner","Version":9,"Enabled":false,"NextAt":null}`},
			{"business.schedule-progress", "owner/exhausted", `{"Version":9,"Consumed":3}`},
			{"business.todos.receipts", "owner/create", `{"Digest":"unchanged","Result":{"ID":"todo","OwnerID":"owner","Version":1,"Title":"original"}}`},
			{"agent.checkpoints", "owner/assistant", `{"OwnerID":"owner","AssistantID":"assistant","FormatVersion":1,"Sequence":8,"ContextStored":true}`},
			{"context.objects", "shared", `{"Kind":"test","Body":"preserve"}`},
		}
		for _, r := range rows {
			if err := tx.Put(ctx, r.bucket, r.key, json.RawMessage(r.raw)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestMigrateV2LifecycleCheckpointAndBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedV2(t, db)
	ctx := t.Context()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	read := func(bucket, key string) map[string]json.RawMessage {
		t.Helper()
		v, err := persistence.Load[map[string]json.RawMessage](ctx, db.Store(), bucket, key)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, tc := range []struct{ bucket, key, state string }{{"business.todos", "owner/todo", "paused"}, {"business.schedules", "owner/disabled", "paused"}, {"business.schedules", "owner/active", "active"}, {"business.watches", "owner/watch", "active"}, {"business.schedules", "owner/exhausted", "active"}} {
		v := read(tc.bucket, tc.key)
		if string(v["State"]) != `"`+tc.state+`"` || v["Paused"] != nil || v["Enabled"] != nil {
			t.Fatal(tc, v)
		}
	}
	if string(read("business.watches", "owner/watch")["RunState"]) != `"blocked"` {
		t.Fatal("runtime state lost")
	}
	if read("business.plan-links", "owner/todo-todo")["Paused"] != nil {
		t.Fatal("duplicate pause field")
	}
	cp := read("agent.checkpoints", "owner/assistant")
	if cp["FormatVersion"] != nil || string(cp["Sequence"]) != "8" {
		t.Fatal("checkpoint changed", cp)
	}
	var replay struct {
		State, Title string
		Version      int
	}
	if err = json.Unmarshal(read("business.todos.receipts", "owner/create")["Result"], &replay); err != nil || replay.State != "paused" || replay.Title != "original" || replay.Version != 1 {
		t.Fatal("old response shape replayed", replay, err)
	}
	schema := read("system", "schema")
	if string(schema["Version"]) != "3" || schema["CheckpointVersion"] != nil {
		t.Fatal(schema)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal("repeat migration", err)
	}
	backup, err := sqlitestore.OpenUninitialized(path + ".pre-migration.db")
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	old, err := persistence.Load[struct{ Version, CheckpointVersion uint32 }](ctx, backup, "system", "schema")
	if err != nil || old.Version != 2 || old.CheckpointVersion != 1 {
		t.Fatal("backup did not preserve old schema", old, err)
	}
}
func TestMigrationRollbackAndUnsupportedVersions(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedV2(t, db)
	injected := errors.New("failure after conversion")
	db.InjectMigrationError(injected)
	if err = db.Migrate(t.Context()); !errors.Is(err, injected) {
		t.Fatal(err)
	}
	old, err := persistence.Load[struct{ Paused bool }](t.Context(), db.Store(), "business.plan-links", "owner/todo-todo")
	if err != nil || !old.Paused {
		t.Fatal("conversion survived rollback", err)
	}
	schema, err := db.Schema(t.Context())
	if err != nil || schema.Version != 2 {
		t.Fatal(schema, err)
	}
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal("retry", err)
	}
	for _, version := range []uint32{1, 99} {
		if err = db.Store().Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, "system", "schema", Schema{Version: version})
		}); err != nil {
			t.Fatal(err)
		}
		if err = db.Migrate(t.Context()); err == nil {
			t.Fatal("unsupported version accepted", version)
		}
	}
}
func TestUnversionedDataAndUnknownCheckpointAreRejected(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "unversioned", true: "checkpoint"}[unknown], func(t *testing.T) {
			db, err := Open(filepath.Join(t.TempDir(), "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			err = db.Store().Write(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
				if unknown {
					return tx.Put(ctx, "system", "schema", map[string]int{"Version": 2, "CheckpointVersion": 9})
				}
				return tx.Put(ctx, "some.module", "record", map[string]string{"value": "preserve"})
			})
			if err != nil {
				t.Fatal(err)
			}
			if err = db.Migrate(t.Context()); err == nil {
				t.Fatal("unknown format accepted")
			}
		})
	}
}
