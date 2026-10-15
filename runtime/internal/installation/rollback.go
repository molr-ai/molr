//go:build linux || darwin

package installation

import (
	"context"
	"errors"
	"fmt"
	"molr.ai/runtime/internal/installrecord"
	"os"
	"path/filepath"
	"time"
)

type savedInstallFile struct {
	path, backup string
	mode         os.FileMode
	exists       bool
}

// Snapshot only installer-owned files. Never copy or roll back the live database.
func snapshotInstallation(r *installrecord.Record) (string, []savedInstallFile, error) {
	dir, err := os.MkdirTemp(r.Paths.Program, ".rollback-")
	if err != nil {
		return "", nil, err
	}
	paths := []string{r.File(), r.Paths.Unit, filepath.Join(r.Paths.Config, "server.json"), filepath.Join(r.Paths.Config, "server.previous.json")}
	for _, name := range programFiles {
		paths = append(paths, filepath.Join(r.Paths.Program, name))
	}
	files := make([]savedInstallFile, 0, len(paths))
	for i, path := range paths {
		f := savedInstallFile{path: path, backup: filepath.Join(dir, fmt.Sprint(i))}
		info, e := os.Lstat(path)
		if e != nil && !os.IsNotExist(e) {
			os.RemoveAll(dir)
			return "", nil, e
		}
		if e == nil {
			if !info.Mode().IsRegular() {
				os.RemoveAll(dir)
				return "", nil, fmt.Errorf("unsafe rollback source")
			}
			f.exists = true
			f.mode = info.Mode().Perm()
			if e = copyFile(f.backup, path, 0600, r.OwnerUID, r.GID); e != nil {
				os.RemoveAll(dir)
				return "", nil, e
			}
		}
		files = append(files, f)
	}
	return dir, files, nil
}
func restoreInstallation(r *installrecord.Record, files []savedInstallFile) error {
	for _, f := range files {
		if f.exists {
			if err := copyFile(f.path, f.backup, f.mode, r.OwnerUID, r.GID); err != nil {
				return err
			}
		} else if err := removeFile(f.path); err != nil {
			return err
		}
	}
	return nil
}
func (e *Engine) rollback(r *installrecord.Record, files []savedInstallFile, delegate, wasRunning bool) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(e.Ctx), 90*time.Second)
	defer cancel()
	worker := *e
	worker.Ctx = ctx
	state, err := worker.unitState(r)
	if err != nil {
		return err
	}
	if err = worker.stopService(r, state, false); err != nil {
		return err
	}
	if err = restoreInstallation(r, files); err != nil {
		return err
	}
	if wasRunning {
		return worker.startService(r, delegate, true)
	}
	return nil
}
func installationFailed(err, errorRecovery error) error {
	if errorRecovery != nil {
		return errors.Join(err, fmt.Errorf("previous installation recovery failed: %w", errorRecovery))
	}
	return fmt.Errorf("update failed; previous installation files and service restored: %w", err)
}
