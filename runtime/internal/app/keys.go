package app

import (
	"crypto/rand"
	"io"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"os"
	"path/filepath"
)

func prepareDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return core.Invalid("data_dir", "real private directory required")
	}
	return platform.PrivateDirectory(path)
}
func masterKey(dir string) ([]byte, error) {
	keydir := filepath.Join(dir, "keys")
	if err := prepareDirectory(keydir); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(keydir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat("master.key")
	if err == nil {
		if !platform.PrivateFile(filepath.Join(keydir, "master.key"), info) {
			return nil, core.NewError(core.FailedPrecondition, "master key must be a private regular file")
		}
		f, err := root.Open("master.key")
		if err != nil {
			return nil, err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, 33))
		if err != nil {
			return nil, err
		}
		if len(b) != 32 {
			return nil, core.NewError(core.FailedPrecondition, "invalid master key")
		}
		return b, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	// Never silently replace a missing key for an existing database.
	if st, err := os.Stat(filepath.Join(dir, "runtime.db")); err == nil && st.Size() > 0 {
		return nil, core.NewError(core.FailedPrecondition, "existing database requires its original master key")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return nil, err
	}
	f, err := root.OpenFile("master.key", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		return masterKey(dir)
	}
	if err != nil {
		return nil, err
	}
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return b, nil
}
