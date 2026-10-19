// Package logging owns Runtime-managed file log storage and retention.
package logging

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type Options struct {
	MaxFileBytes int64 `json:"max_file_bytes"`
	BackupFiles  int   `json:"backup_files"`
}

func DefaultOptions() Options { return Options{MaxFileBytes: 256 << 20, BackupFiles: 1} }

func (o Options) Validate() error {
	if o.MaxFileBytes <= 0 {
		return fmt.Errorf("max_file_bytes must be positive")
	}
	if o.BackupFiles < 0 || o.BackupFiles > 100 {
		return fmt.Errorf("backup_files must be between 0 and 100")
	}
	return nil
}

// File serializes complete writes and rotates before a write would exceed the
// configured limit. Share one File per path within a Runtime; multiple processes
// must use separate paths. Each write closes its descriptor, including failures.
type File struct {
	mu          sync.Mutex
	path        string
	options     Options
	initialized bool
}

// NewFile is lazy: disabled diagnostics never create directories or files.
func NewFile(path string, options Options) (*File, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	if path == "" {
		return nil, fmt.Errorf("log path required")
	}
	return &File{path: path, options: options}, nil
}

// OpenFile also checks the destination and applies retention immediately, so
// an unavailable service log fails startup rather than silently losing logs.
func OpenFile(path string, options Options) (*File, error) {
	f, err := NewFile(path, options)
	if err != nil {
		return nil, err
	}
	_, err = f.Write(nil)
	return f, err
}

func regularFile(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("log path must be a regular file: %s", path)
	}
	return info, nil
}

// Prune only our numbered archives. It also applies a reduced policy after a
// restart, leaving unrelated files alone. No background cleanup loop is needed.
func (f *File) prune() error {
	entries, err := os.ReadDir(filepath.Dir(f.path))
	if err != nil {
		return err
	}
	prefix := filepath.Base(f.path) + "."
	for _, entry := range entries {
		suffix, ok := strings.CutPrefix(entry.Name(), prefix)
		if !ok {
			continue
		}
		n, err := strconv.Atoi(suffix)
		if err != nil || n < 1 || strconv.Itoa(n) != suffix {
			continue
		}
		path := filepath.Join(filepath.Dir(f.path), entry.Name())
		info, err := regularFile(path)
		if err != nil {
			return err
		}
		if info == nil {
			continue
		}
		if n > f.options.BackupFiles || info.Size() > f.options.MaxFileBytes {
			if err := os.Remove(path); err != nil {
				return err
			}
		} else if err := os.Chmod(path, 0600); err != nil {
			return err
		}
	}
	return nil
}

func (f *File) rotate() error {
	if err := f.prune(); err != nil {
		return err
	}
	if f.options.BackupFiles == 0 {
		return os.Remove(f.path)
	}
	oldest := f.path + "." + strconv.Itoa(f.options.BackupFiles)
	if err := os.Remove(oldest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for i := f.options.BackupFiles - 1; i >= 0; i-- {
		source := f.path
		if i > 0 {
			source += "." + strconv.Itoa(i)
		}
		if err := os.Rename(source, f.path+"."+strconv.Itoa(i+1)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return f.prune()
}

func (f *File) Write(data []byte) (int, error) {
	if int64(len(data)) > f.options.MaxFileBytes {
		return 0, fmt.Errorf("log record exceeds max_file_bytes")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(f.path), 0700); err != nil {
		return 0, err
	}
	info, err := regularFile(f.path)
	if err != nil {
		return 0, err
	}
	if !f.initialized {
		if err := f.prune(); err != nil {
			return 0, err
		}
	}
	if info != nil && info.Size() > f.options.MaxFileBytes-int64(len(data)) {
		if err := os.Chmod(f.path, 0600); err != nil {
			return 0, err
		}
		if err := f.rotate(); err != nil {
			return 0, err
		}
	}
	file, err := os.OpenFile(f.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return 0, err
	}
	if err := file.Chmod(0600); err != nil {
		return 0, errors.Join(err, file.Close())
	}
	n, err := file.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	err = errors.Join(err, file.Close())
	f.initialized = err == nil
	return n, err
}
