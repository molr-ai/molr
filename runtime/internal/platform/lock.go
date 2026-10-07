package platform

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type FileLocker struct{}

func NewLocker() *FileLocker { return &FileLocker{} }

type fileLock struct {
	file *os.File
	once sync.Once
	err  error
}

func (l *FileLocker) Acquire(ctx context.Context, directory string) (Lock, error) {
	if err := persistence.CheckContext(ctx); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(directory) {
		return nil, core.Invalid("data_dir", "absolute path required")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if info, err := root.Lstat("server.lock"); err == nil && !info.Mode().IsRegular() {
		return nil, core.NewError(core.Forbidden, "invalid lock file")
	}
	file, err := root.OpenFile("server.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(file); err != nil {
		_ = file.Close()
		return nil, core.Wrap(core.Conflict, "another server owns the data directory", err)
	}
	return &fileLock{file: file}, nil
}
func (l *fileLock) Close() error {
	l.once.Do(func() { _ = unlockFile(l.file); l.err = l.file.Close() })
	return l.err
}
