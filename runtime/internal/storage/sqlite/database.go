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

const schemaVersion uint32 = 3

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
	migrationMu    sync.Mutex
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
	engine, err := sqlitestore.OpenUninitialized(path)
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
func (d *DB) Schema(ctx context.Context) (Schema, error) {
	value, err := persistence.Load[Schema](ctx, d.engine, "system", "schema")
	if persistence.Missing(err) {
		return Schema{}, nil
	}
	return value, err
}
func supportedSchema(s Schema) bool {
	return s.Version == schemaVersion
}

// Migrate runs known steps in order before opening derived query structures.
func (d *DB) Migrate(ctx context.Context) error {
	d.migrationMu.Lock()
	defer d.migrationMu.Unlock()
	current, err := d.Schema(ctx)
	if err != nil {
		return err
	}
	steps := map[uint32]func(context.Context, persistence.Tx) error{2: migratePlans}
	if current.Version > schemaVersion {
		return core.NewError(core.FailedPrecondition, "database is newer than this server")
	}
	if current.Version == 0 {
		empty, err := d.engine.Empty(ctx)
		if err != nil {
			return err
		}
		if !empty {
			return core.NewError(core.FailedPrecondition, "unversioned database requires explicit conversion")
		}
	} else {
		for v := current.Version; v < schemaVersion; v++ {
			if steps[v] == nil {
				return core.NewError(core.FailedPrecondition, "database requires a supported migration")
			}
		}
	}
	if current.Version == 2 {
		if err := d.validateV2(ctx); err != nil {
			return err
		}
	}
	if current.Version != 0 && current.Version < schemaVersion {
		if err := d.backupBeforeMigration(ctx); err != nil {
			return err
		}
	}
	if current.Version == 0 {
		err = d.engine.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			if err := tx.Put(ctx, "system", "schema", Schema{Version: schemaVersion}); err != nil {
				return err
			}
			return d.migrationError()
		})
		if err != nil {
			return err
		}
	}
	for v := current.Version; v != 0 && v < schemaVersion; v++ {
		err = d.engine.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			if err := steps[v](ctx, tx); err != nil {
				return err
			}
			if err := tx.Put(ctx, "system", "schema", Schema{Version: v + 1}); err != nil {
				return err
			}
			return d.migrationError()
		})
		if err != nil {
			return err
		}
	}
	return d.engine.Initialize()
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
