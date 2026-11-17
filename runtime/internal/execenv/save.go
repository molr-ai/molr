package execenv

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"

	"molr.ai/runtime/internal/textfiles"
)

const lockName = ".execution-environment.lock"

// Save serializes installation and CLI updates independently of the running
// server's database lock. The directory must already belong to the instance.
func Save(ctx context.Context, directory string, selected map[string]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validate(selected); err != nil {
		return err
	}
	parent, err := textfiles.OpenDirectory(directory, ".")
	if err != nil {
		return err
	}
	defer parent.Close()
	owner, err := parent.Stat()
	if err != nil {
		return err
	}
	if err := checkOwner(owner); err != nil {
		return err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	if info, err := root.Stat("."); err != nil || !os.SameFile(owner, info) {
		return fmt.Errorf("execution environment directory changed")
	}
	lock, err := openLock(root, owner)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := lockFile(lock); err != nil {
		return fmt.Errorf("execution environment is being updated; retry: %w", err)
	}
	defer unlockFile(lock)
	data, err := EncodeUpdate(directory, selected)
	if err != nil {
		return err
	}
	temporary := ".execution-environment-" + rand.Text()
	f, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	defer root.Remove(temporary)
	if err := setOwner(f, owner); err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	current, err := textfiles.OpenDirectory(directory, ".")
	if err != nil {
		return err
	}
	info, statErr := current.Stat()
	current.Close()
	if statErr != nil || !os.SameFile(owner, info) {
		return fmt.Errorf("execution environment directory changed")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = root.Rename(temporary, Filename); err != nil {
		return err
	}
	return syncDirectory(parent)
}

func openLock(root *os.Root, owner os.FileInfo) (*os.File, error) {
	f, err := root.OpenFile(lockName, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err == nil {
		if err = setOwner(f, owner); err != nil {
			f.Close()
			return nil, err
		}
		return f, nil
	}
	if !os.IsExist(err) {
		return nil, err
	}
	before, err := root.Lstat(lockName)
	if err != nil || !before.Mode().IsRegular() {
		return nil, fmt.Errorf("execution environment lock must be a private regular file")
	}
	f, err = root.OpenFile(lockName, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) || !privateLock(f, after, owner) {
		f.Close()
		return nil, fmt.Errorf("execution environment lock changed or is not private")
	}
	return f, nil
}
