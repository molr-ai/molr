//go:build !windows

package execenv

import (
	"fmt"
	"os"
	"syscall"
)

func checkOwner(info os.FileInfo) error {
	owner := info.Sys().(*syscall.Stat_t)
	if info.Mode().Perm()&0022 != 0 || os.Geteuid() != 0 && uint32(os.Geteuid()) != owner.Uid {
		return fmt.Errorf("run as the service account or root with a protected data directory")
	}
	return nil
}

func setOwner(f *os.File, owner os.FileInfo) error {
	if err := f.Chmod(0600); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		stat := owner.Sys().(*syscall.Stat_t)
		return f.Chown(int(stat.Uid), int(stat.Gid))
	}
	return nil
}

func privateLock(_ *os.File, info, owner os.FileInfo) bool {
	stat := info.Sys().(*syscall.Stat_t)
	return info.Mode().IsRegular() && info.Mode().Perm()&0077 == 0 && stat.Nlink == 1 && stat.Uid == owner.Sys().(*syscall.Stat_t).Uid
}

func lockFile(f *os.File) error      { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) }
func unlockFile(f *os.File)          { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
func syncDirectory(f *os.File) error { return f.Sync() }
