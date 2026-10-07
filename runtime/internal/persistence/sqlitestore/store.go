// Package sqlitestore implements the storage-neutral transaction boundary using
// a real SQLite database. Domain packages never import this package.
package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	_ "modernc.org/sqlite"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type Store struct {
	db          *sql.DB
	gate        chan struct{}
	closed      atomic.Bool
	faultMu     sync.Mutex
	commitFault error
	path        string
}

type contextKey struct{}
type transaction struct {
	store     *Store
	sql       *sql.Tx
	ctx       context.Context
	writable  bool
	active    atomic.Bool
	savepoint uint64
}
type boundStore struct{ tx *transaction }

// Open creates the private KV backing schema. Higher-level schema/version
// migrations are managed separately by storage/sqlite.Database.
func Open(path string) (*Store, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, core.Invalid("database_path", "absolute path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return nil, core.Invalid("database_path", "regular file required")
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	dsn := (&url.URL{Scheme: "file", Path: path}).String() + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS records (bucket TEXT NOT NULL, key TEXT NOT NULL, value BLOB NOT NULL, PRIMARY KEY(bucket,key))`); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db, gate: make(chan struct{}, 1), path: path}
	if err := s.createIndexes(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Path() string { return s.path }
func (s *Store) Close() error {
	if s.closed.Swap(true) {
		return nil
	}
	s.gate <- struct{}{}
	defer func() { <-s.gate }()
	return s.db.Close()
}
func (s *Store) FailNextCommit(err error) {
	s.faultMu.Lock()
	defer s.faultMu.Unlock()
	s.commitFault = err
}
func (s *Store) takeFault() error {
	s.faultMu.Lock()
	defer s.faultMu.Unlock()
	err := s.commitFault
	s.commitFault = nil
	return err
}
func (s *Store) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.run(ctx, false, fn)
}
func (s *Store) Write(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return s.run(ctx, true, fn)
}
func (s *Store) run(ctx context.Context, write bool, fn func(context.Context, persistence.Tx) error) error {
	if err := persistence.CheckContext(ctx); err != nil {
		return err
	}
	if fn == nil {
		return core.Invalid("callback", "required")
	}
	if s.closed.Load() {
		return core.NewError(core.Unavailable, "database closed")
	}
	if tx, ok := ctx.Value(contextKey{}).(*transaction); ok && tx.store == s {
		return tx.run(ctx, write, fn)
	}
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	case <-ctx.Done():
		return persistence.CheckContext(ctx)
	}
	if s.closed.Load() {
		return core.NewError(core.Unavailable, "database closed")
	}
	sqlTx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: !write})
	if err != nil {
		return translate(ctx, err)
	}
	tx := &transaction{store: s, sql: sqlTx, ctx: ctx, writable: write}
	tx.active.Store(true)
	defer func() { tx.active.Store(false); _ = sqlTx.Rollback() }()
	txCtx := context.WithValue(ctx, contextKey{}, tx)
	if err = fn(txCtx, tx); err != nil {
		return err
	}
	if err = persistence.CheckContext(ctx); err != nil {
		return err
	}
	if write {
		if err = s.takeFault(); err != nil {
			return err
		}
	}
	return translate(ctx, sqlTx.Commit())
}
func (tx *transaction) run(ctx context.Context, write bool, fn func(context.Context, persistence.Tx) error) error {
	if err := tx.check(ctx); err != nil {
		return err
	}
	if write && !tx.writable {
		return core.NewError(core.FailedPrecondition, "cannot write in a read transaction")
	}
	nested := context.WithValue(ctx, contextKey{}, tx)
	if !write {
		return fn(nested, tx)
	}
	tx.savepoint++
	name := fmt.Sprintf("nested_%d", tx.savepoint)
	if _, err := tx.sql.ExecContext(ctx, "SAVEPOINT "+name); err != nil {
		return translate(ctx, err)
	}
	err := fn(nested, tx)
	if err == nil {
		err = persistence.CheckContext(ctx)
	}
	if err != nil {
		_, _ = tx.sql.ExecContext(ctx, "ROLLBACK TO "+name)
		_, _ = tx.sql.ExecContext(ctx, "RELEASE "+name)
		return err
	}
	_, err = tx.sql.ExecContext(ctx, "RELEASE "+name)
	return translate(ctx, err)
}
func (tx *transaction) check(ctx context.Context) error {
	if !tx.active.Load() {
		return core.NewError(core.FailedPrecondition, "transaction already ended")
	}
	if err := persistence.CheckContext(ctx); err != nil {
		return err
	}
	return persistence.CheckContext(tx.ctx)
}
func (tx *transaction) Store() persistence.Store { return boundStore{tx: tx} }
func (b boundStore) Read(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return b.tx.run(ctx, false, fn)
}
func (b boundStore) Write(ctx context.Context, fn func(context.Context, persistence.Tx) error) error {
	return b.tx.run(ctx, true, fn)
}
func (tx *transaction) Get(ctx context.Context, bucket, key string, dest any) error {
	if err := tx.check(ctx); err != nil {
		return err
	}
	if err := valid(bucket, key); err != nil {
		return err
	}
	var data []byte
	err := tx.sql.QueryRowContext(ctx, "SELECT value FROM records WHERE bucket=? AND key=?", bucket, key).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return core.NewError(core.NotFound, "record not found")
	}
	if err != nil {
		return translate(ctx, err)
	}
	if err = json.Unmarshal(data, dest); err != nil {
		return core.Wrap(core.Internal, "invalid stored record", err)
	}
	return nil
}
func (tx *transaction) Put(ctx context.Context, bucket, key string, value any) error {
	if err := tx.check(ctx); err != nil {
		return err
	}
	if !tx.writable {
		return core.NewError(core.FailedPrecondition, "read-only transaction")
	}
	if err := valid(bucket, key); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return core.Wrap(core.InvalidArgument, "cannot encode stored value", err)
	}
	limit := 32 << 20
	if strings.HasPrefix(bucket, "agent.") {
		limit = 256 << 20
	}
	if len(data) > limit {
		return core.Invalid("record", "record too large")
	}
	_, err = tx.sql.ExecContext(ctx, "INSERT INTO records(bucket,key,value) VALUES(?,?,?) ON CONFLICT(bucket,key) DO UPDATE SET value=excluded.value", bucket, key, data)
	return translate(ctx, err)
}
func (tx *transaction) Delete(ctx context.Context, bucket, key string) error {
	if err := tx.check(ctx); err != nil {
		return err
	}
	if !tx.writable {
		return core.NewError(core.FailedPrecondition, "read-only transaction")
	}
	if err := valid(bucket, key); err != nil {
		return err
	}
	result, err := tx.sql.ExecContext(ctx, "DELETE FROM records WHERE bucket=? AND key=?", bucket, key)
	if err != nil {
		return translate(ctx, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return core.NewError(core.NotFound, "record not found")
	}
	return nil
}
func (tx *transaction) Scan(ctx context.Context, bucket string) ([]persistence.Record, error) {
	if err := tx.check(ctx); err != nil {
		return nil, err
	}
	if err := valid(bucket, "scan"); err != nil {
		return nil, err
	}
	rows, err := tx.sql.QueryContext(ctx, "SELECT key,value FROM records WHERE bucket=? ORDER BY key", bucket)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	result := []persistence.Record{}
	for rows.Next() {
		var row persistence.Record
		if err := rows.Scan(&row.Key, &row.Data); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, translate(ctx, rows.Err())
}
func valid(bucket, key string) error {
	if bucket == "" || len(bucket) > 128 || strings.ContainsRune(bucket, 0) || key == "" || len(key) > 4096 || strings.ContainsRune(key, 0) {
		return core.Invalid("record_key", "invalid bucket or key")
	}
	return nil
}
func translate(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return persistence.CheckContext(ctx)
	}
	if strings.Contains(err.Error(), "constraint") {
		return core.Wrap(core.Conflict, "storage conflict", err)
	}
	return core.Wrap(core.Unavailable, "storage operation failed", err)
}

// Snapshot creates a consistent SQLite copy outside a transaction callback.
func (s *Store) Snapshot(ctx context.Context, path string) error {
	if !filepath.IsAbs(path) {
		return core.Invalid("snapshot_path", "absolute path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	case <-ctx.Done():
		return persistence.CheckContext(ctx)
	}
	if s.closed.Load() {
		return core.NewError(core.Unavailable, "database closed")
	}
	if _, err := os.Lstat(path); err == nil {
		return core.NewError(core.Conflict, "snapshot already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	// The path is passed as a SQL value, never concatenated into the statement.
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		return translate(ctx, err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("protect snapshot: %w", err)
	}
	return nil
}
