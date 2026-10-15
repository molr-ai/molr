package installation

import (
	"debug/macho"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func checkExecutable(path, arch string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0755 {
		return fmt.Errorf("protected executable with mode 0755 required: %s", path)
	}
	f, err := macho.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if f.Type != macho.TypeExec || (arch == "arm64" && f.Cpu != macho.CpuArm64) || (arch == "amd64" && f.Cpu != macho.CpuAmd64) {
		return fmt.Errorf("wrong Mach-O executable architecture")
	}
	if err := exec.Command("/usr/bin/codesign", "--verify", "--strict", path).Run(); err != nil {
		return fmt.Errorf("invalid executable code signature: %w", err)
	}
	return nil
}
func checkTree(path string, uid int) error {
	if err := checkPath(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || int(info.Sys().(*syscall.Stat_t).Uid) != uid {
		return fmt.Errorf("directory ownership changed: %s", path)
	}
	n, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil {
		return err
	}
	mounts := make([]unix.Statfs_t, n+16)
	n, err = unix.Getfsstat(mounts, unix.MNT_NOWAIT)
	if err != nil {
		return err
	}
	for _, m := range mounts[:n] {
		mount := unix.ByteSliceToString(m.Mntonname[:])
		if mount == path || strings.HasPrefix(mount, path+"/") {
			return fmt.Errorf("unmount filesystem before removal: %s", mount)
		}
	}
	return nil
}
func removeTree(path string) error {
	parent, err := openDir(filepath.Dir(path), false)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer parent.Close()
	var st unix.Stat_t
	if err = unix.Fstat(int(parent.Fd()), &st); err != nil {
		return err
	}
	return removeAt(int(parent.Fd()), filepath.Base(path), st.Dev)
}

// Never follows links, and refuses to descend onto another filesystem.
func removeAt(parent int, name string, device int32) error {
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err == unix.ENOENT {
		return nil
	}
	if err == unix.ENOTDIR || err == unix.ELOOP {
		return unix.Unlinkat(parent, name, 0)
	}
	if err != nil {
		return err
	}
	dir := os.NewFile(uintptr(fd), name)
	defer dir.Close()
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Dev != device {
		return fmt.Errorf("refusing mounted directory")
	}
	names, err := dir.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, child := range names {
		if err = removeAt(fd, child, device); err != nil {
			return err
		}
	}
	var current unix.Stat_t
	if err = unix.Fstatat(parent, name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if current.Dev != st.Dev || current.Ino != st.Ino {
		return fmt.Errorf("directory changed during removal")
	}
	return unix.Unlinkat(parent, name, unix.AT_REMOVEDIR)
}
