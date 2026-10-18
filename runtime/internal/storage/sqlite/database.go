package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/platform"
)

const schemaVersion uint32 = 1
const checkpointVersion uint32 = 1

// DB owns the engine and an OS-backed lock shared with offline restore. The
// application data-directory lock can be held independently of this DB lock.
type DB struct {
	engine         *sqlitestore.Store
	path           string
	lock           platform.Lock
	closed         atomic.Bool
	closeOnce      sync.Once
	closeErr       error
	faultMu        sync.Mutex
	migrationFault error
}

func Open(path string) (*DB, error) {
	if !filepath.IsAbs(path) {
		return nil, core.Invalid("database_path", "absolute path required")
	}
	path = filepath.Clean(path)
	lock, err := platform.NewLocker().Acquire(context.Background(), lockDirectory(path))
	if err != nil {
		return nil, err
	}
	engine, err := sqlitestore.Open(path)
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	return &DB{engine: engine, path: path, lock: lock}, nil
}
func lockDirectory(path string) string {
	return filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".instance")
}
func (d *DB) Store() persistence.Store { return d.engine }
func (d *DB) Close() error {
	d.closeOnce.Do(func() { d.closed.Store(true); d.closeErr = errors.Join(d.engine.Close(), d.lock.Close()) })
	return d.closeErr
}
func (d *DB) FailNextCommit(err error) { d.engine.FailNextCommit(err) }
func (d *DB) InjectMigrationError(err error) {
	d.faultMu.Lock()
	d.migrationFault = err
	d.faultMu.Unlock()
}
func (d *DB) migrationError() error {
	d.faultMu.Lock()
	defer d.faultMu.Unlock()
	err := d.migrationFault
	d.migrationFault = nil
	return err
}
func (d *DB) Within(ctx context.Context, fn func(Repositories) error) error {
	if fn == nil {
		return core.Invalid("transaction", "callback required")
	}
	return d.engine.Write(ctx, func(_ context.Context, tx persistence.Tx) error { return fn(&repositories{store: tx.Store()}) })
}
func (d *DB) Schema(ctx context.Context) (Schema, error) {
	value, err := persistence.Load[Schema](ctx, d.engine, "system", "schema")
	if persistence.Missing(err) {
		return Schema{}, nil
	}
	return value, err
}
func supportedSchema(s Schema) bool {
	return s.Version == schemaVersion && s.CheckpointVersion == checkpointVersion
}
func (d *DB) Migrate(ctx context.Context) error {
	return d.engine.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		current, err := d.Schema(ctx)
		if err != nil {
			return err
		}
		if supportedSchema(current) {
			return nil
		}
		if current != (Schema{}) {
			return core.NewError(core.FailedPrecondition, "database schema is not supported by this server")
		}
		if err = tx.Put(ctx, "system", "schema", Schema{Version: schemaVersion, CheckpointVersion: checkpointVersion}); err != nil {
			return err
		}
		return d.migrationError()
	})
}
func (d *DB) Check(ctx context.Context) error {
	current, err := d.Schema(ctx)
	if err != nil {
		return err
	}
	if !supportedSchema(current) {
		return core.NewError(core.FailedPrecondition, "database requires a supported migration")
	}
	return checkFile(ctx, d.path)
}
func checkFile(ctx context.Context, path string) error {
	dsn := sqlitestore.FileURL(path) + "?mode=ro"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	var result string
	if err = db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil {
		return core.Wrap(core.Unavailable, "database integrity check failed", err)
	}
	if result != "ok" {
		return core.NewError(core.FailedPrecondition, "database integrity check failed")
	}
	return nil
}
func opaqueID() core.ID {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(err)
	}
	return core.ID(hex.EncodeToString(bytes[:]))
}
