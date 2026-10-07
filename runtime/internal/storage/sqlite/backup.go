package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/platform"
)

type backupManifest struct {
	Backup Backup
	Size   int64
	SHA256 string
}
type restorer struct{ path string }

func backupDirectory(path string) string {
	return filepath.Join(filepath.Dir(path), "backups", filepath.Base(path))
}

func regularFile(root *os.Root, name string) (*os.File, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, core.NewError(core.Forbidden, "regular file required")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !os.SameFile(before, after) {
		_ = file.Close()
		return nil, core.NewError(core.Forbidden, "file changed while opening")
	}
	return file, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := persistence.CheckContext(r.ctx); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
func (d *DB) Backup(ctx context.Context) (Backup, error) {
	schema, err := d.Schema(ctx)
	if err != nil {
		return Backup{}, err
	}
	if !supportedSchema(schema) {
		return Backup{}, core.NewError(core.FailedPrecondition, "migrate before backup")
	}
	directory := backupDirectory(d.path)
	if err = os.MkdirAll(directory, 0700); err != nil {
		return Backup{}, err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return Backup{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Backup{}, core.NewError(core.Forbidden, "private backup directory required")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return Backup{}, err
	}
	defer root.Close()
	result := Backup{ID: opaqueID(), Schema: schema, CreatedAt: time.Now().UTC()}
	name := string(result.ID) + ".db"
	manifestName := string(result.ID) + ".json"
	if err = d.engine.Snapshot(ctx, filepath.Join(directory, name)); err != nil {
		return Backup{}, err
	}
	successful := false
	defer func() {
		if !successful {
			_ = root.Remove(name)
			_ = root.Remove(manifestName)
		}
	}()
	snapshot, err := regularFile(root, name)
	if err != nil {
		return Backup{}, err
	}
	hash := sha256.New()
	size, err := io.Copy(hash, contextReader{ctx, snapshot})
	if err == nil {
		err = snapshot.Sync()
	}
	closeErr := snapshot.Close()
	if err != nil {
		return Backup{}, err
	}
	if closeErr != nil {
		return Backup{}, closeErr
	}
	metadata := backupManifest{Backup: result, Size: size, SHA256: hex.EncodeToString(hash.Sum(nil))}
	file, err := root.OpenFile(manifestName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return Backup{}, err
	}
	err = json.NewEncoder(file).Encode(metadata)
	if err == nil {
		err = file.Sync()
	}
	closeErr = file.Close()
	if err != nil {
		return Backup{}, err
	}
	if closeErr != nil {
		return Backup{}, closeErr
	}
	successful = true
	return result, nil
}

func NewRestorer(path string) (Restorer, error) {
	if !filepath.IsAbs(path) {
		return nil, core.Invalid("database_path", "absolute path required")
	}
	return &restorer{path: filepath.Clean(path)}, nil
}
func (r *restorer) Restore(ctx context.Context, backup Backup) error {
	if backup.ID.Validate() != nil || !supportedSchema(backup.Schema) || backup.CreatedAt.IsZero() {
		return core.Invalid("backup", "invalid or unsupported snapshot metadata")
	}
	lock, err := platform.NewLocker().Acquire(ctx, lockDirectory(r.path))
	if err != nil {
		if core.CodeOf(err) == core.Conflict {
			return core.NewError(core.FailedPrecondition, "database must be closed before restore")
		}
		return err
	}
	defer lock.Close()
	// A live or crash-left WAL cannot safely be replaced by renaming only the DB.
	// Refuse rather than apply an old sidecar to a different restored database.
	for _, suffix := range []string{"-wal", "-shm"} {
		info, err := os.Lstat(r.path + suffix)
		if err == nil {
			if !info.Mode().IsRegular() || info.Size() != 0 {
				return core.NewError(core.FailedPrecondition, "checkpoint database sidecars before restore")
			}
			if err = os.Remove(r.path + suffix); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if info, err := os.Lstat(r.path); err == nil {
		if !info.Mode().IsRegular() {
			return core.NewError(core.Forbidden, "regular database file required")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	directory := backupDirectory(r.path)
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return core.NewError(core.Forbidden, "private backup directory required")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	manifestFile, err := regularFile(root, string(backup.ID)+".json")
	if err != nil {
		return err
	}
	var metadata backupManifest
	decoder := json.NewDecoder(io.LimitReader(manifestFile, 64<<10))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&metadata)
	if err == nil {
		var extra any
		if next := decoder.Decode(&extra); next != io.EOF {
			err = core.Invalid("backup", "trailing manifest content")
		}
	}
	closeErr := manifestFile.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if metadata.Backup.ID != backup.ID || metadata.Backup.Schema != backup.Schema || !metadata.Backup.CreatedAt.Equal(backup.CreatedAt) || metadata.Size <= 0 {
		return core.Invalid("backup", "snapshot metadata mismatch")
	}
	source, err := regularFile(root, string(backup.ID)+".db")
	if err != nil {
		return err
	}
	defer source.Close()
	temp, err := os.CreateTemp(filepath.Dir(r.path), ".restore-*.db")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	cleanPath := tempPath + ".clean"
	defer func() {
		_ = os.Remove(tempPath)
		_ = os.Remove(tempPath + "-wal")
		_ = os.Remove(tempPath + "-shm")
		_ = os.Remove(cleanPath)
	}()
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temp, hash), contextReader{ctx, source})
	if copyErr == nil {
		copyErr = temp.Sync()
	}
	closeErr = temp.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != metadata.Size || hex.EncodeToString(hash.Sum(nil)) != metadata.SHA256 {
		return core.NewError(core.FailedPrecondition, "snapshot checksum mismatch")
	}
	if err = checkFile(ctx, tempPath); err != nil {
		return err
	}
	candidate, err := sqlitestore.Open(tempPath)
	if err != nil {
		return err
	}
	defer candidate.Close()
	schema, err := persistence.Load[Schema](ctx, candidate, "system", "schema")
	if err != nil {
		return err
	}
	if schema != backup.Schema || !supportedSchema(schema) {
		return core.NewError(core.FailedPrecondition, "snapshot schema is incompatible")
	}
	if err = candidate.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		for _, bucket := range []string{"auth.sessions", "auth.tokens", "auth.login-links", "auth.challenges", "auth.totp_used"} {
			records, err := tx.Scan(ctx, bucket)
			if err != nil {
				return err
			}
			for _, record := range records {
				if err = tx.Delete(ctx, bucket, record.Key); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}
	// VACUUM INTO materializes the sanitized candidate without carrying WAL files.
	if err = candidate.Snapshot(ctx, cleanPath); err != nil {
		return err
	}
	if err = candidate.Close(); err != nil {
		return err
	}
	clean, err := os.OpenFile(cleanPath, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	err = clean.Sync()
	closeErr = clean.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = persistence.CheckContext(ctx); err != nil {
		return err
	}
	if err = os.Rename(cleanPath, r.path); err != nil {
		return err
	}
	return nil
}

var _ Database = (*DB)(nil)
var _ Restorer = (*restorer)(nil)
