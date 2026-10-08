package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type ObjectStore struct {
	root          *os.Root
	store         persistence.Store
	mu            sync.Mutex
	commitFailure error
}

func NewObjects(directory string, store persistence.Store) (*ObjectStore, error) {
	if !filepath.IsAbs(directory) {
		return nil, core.Invalid("object_directory", "absolute path required")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"objects", "staging"} {
		if err := root.MkdirAll(name, 0700); err != nil {
			_ = root.Close()
			return nil, err
		}
	}
	return &ObjectStore{root: root, store: store}, nil
}
func (o *ObjectStore) Close() error { return o.root.Close() }
func (o *ObjectStore) FailNextCommit(err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.commitFailure = err
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
func (o *ObjectStore) Put(ctx context.Context, id core.ID, reader io.Reader, limit int64) (StagedObject, error) {
	if err := persistence.CheckContext(ctx); err != nil {
		return StagedObject{}, err
	}
	if err := id.Validate(); err != nil {
		return StagedObject{}, err
	}
	if reader == nil || limit < 0 || limit > 1<<40 {
		return StagedObject{}, core.Invalid("upload", "invalid stream or byte limit")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	name := "staging/" + string(id)
	if _, err := o.root.Lstat("objects/" + string(id)); err == nil {
		return StagedObject{}, persistence.Conflict("object already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return StagedObject{}, err
	}
	file, err := o.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return StagedObject{}, core.Wrap(core.Conflict, "cannot create staged object", err)
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(contextReader{ctx, reader}, limit+1))
	if copyErr == nil && n > limit {
		copyErr = core.Invalid("size", "upload exceeds byte limit")
	}
	if copyErr == nil {
		copyErr = file.Sync()
	}
	closeErr := file.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		_ = o.root.Remove(name)
		return StagedObject{}, copyErr
	}
	return StagedObject{ID: id, Size: n, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}
func (o *ObjectStore) verify(name string, object StagedObject) error {
	info, err := o.root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return core.NewError(core.Forbidden, "object is not a regular file")
	}
	file, err := o.root.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, file)
	if err != nil {
		return err
	}
	if n != object.Size || hex.EncodeToString(hash.Sum(nil)) != object.SHA256 {
		return core.NewError(core.Conflict, "object integrity mismatch")
	}
	return nil
}
func (o *ObjectStore) Commit(ctx context.Context, object StagedObject) error {
	if err := persistence.CheckContext(ctx); err != nil {
		return err
	}
	if err := object.ID.Validate(); err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.commitFailure != nil {
		err := o.commitFailure
		o.commitFailure = nil
		return err
	}
	target := "objects/" + string(object.ID)
	if _, err := o.root.Lstat(target); err == nil {
		return o.verify(target, object)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	staged := "staging/" + string(object.ID)
	if err := o.verify(staged, object); err != nil {
		return err
	}
	return o.root.Rename(staged, target)
}
func (o *ObjectStore) Open(ctx context.Context, id core.ID) (io.ReadCloser, error) {
	if err := persistence.CheckContext(ctx); err != nil {
		return nil, err
	}
	if err := id.Validate(); err != nil {
		return nil, err
	}
	name := "objects/" + string(id)
	info, err := o.root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, core.NewError(core.NotFound, "object not found")
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, core.NewError(core.Forbidden, "object is not a regular file")
	}
	f, err := o.root.Open(name)
	if err != nil {
		return nil, core.Wrap(core.Forbidden, "cannot open protected object", err)
	}
	return f, nil
}
func (o *ObjectStore) Remove(ctx context.Context, id core.ID) error {
	if err := persistence.CheckContext(ctx); err != nil {
		return err
	}
	if err := id.Validate(); err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, prefix := range []string{"objects/", "staging/"} {
		if err := o.root.Remove(prefix + string(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// Recover is a startup/offline operation; callers must not admit uploads until
// it completes. Only metadata-confirmed ready objects survive recovery.
func (o *ObjectStore) Recover(ctx context.Context) error {
	if err := persistence.CheckContext(ctx); err != nil {
		return err
	}
	ready := map[string]bool{}
	if o.store != nil {
		rows, err := persistence.Select[File](ctx, o.store, Bucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("State", Ready)}})
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.State == Ready {
				ready[string(row.ID)] = true
			}
		}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, dir := range []string{"staging", "objects"} {
		handle, err := o.root.Open(dir)
		if err != nil {
			return err
		}
		entries, err := handle.ReadDir(-1)
		_ = handle.Close()
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := persistence.CheckContext(ctx); err != nil {
				return err
			}
			if dir == "objects" && ready[entry.Name()] {
				continue
			}
			if err := o.root.RemoveAll(dir + "/" + entry.Name()); err != nil {
				return err
			}
		}
	}
	return nil
}
