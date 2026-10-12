package installation

import (
	"crypto/rand"
	"debug/elf"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func checkPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" || strings.ContainsAny(path, "\x00\r\n") {
		return fmt.Errorf("dedicated clean absolute path required: %s", path)
	}
	for p := path; p != "/"; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlinked path: %s", p)
		}
	}
	return nil
}
func overlaps(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}
func separate(paths ...string) error {
	for i, a := range paths {
		for _, b := range paths[i+1:] {
			if overlaps(a, b) {
				return fmt.Errorf("installation paths overlap: %s, %s", a, b)
			}
		}
	}
	return nil
}

type permissionChoice struct {
	path   string
	info   os.FileInfo
	action string
}

func checkManaged(path string, uid int, choices map[string]*permissionChoice) error {
	if err := checkPath(path); err != nil {
		return err
	}
	for p := path; p != "/"; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		st := info.Sys().(*syscall.Stat_t)
		if info.IsDir() && st.Uid == 0 && info.Mode()&os.ModeSticky != 0 {
			continue
		}
		if int(st.Uid) != uid && st.Uid != 0 {
			return fmt.Errorf("path owner UID %d, expected %d or root: %s", st.Uid, uid, p)
		}
		if info.Mode().Perm()&0022 != 0 {
			if !info.IsDir() || choices == nil {
				return fmt.Errorf("path must not be group/other writable: %s", p)
			}
			if choices[p] == nil {
				choices[p] = &permissionChoice{path: p, info: info}
			}
		}
	}
	return nil
}

// openDir pins every ancestor without following symlinks. All writes are relative to this descriptor.
func openDir(path string, create bool) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("invalid directory: %s", path)
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if part == "" {
			continue
		}
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if e == unix.ENOENT && create {
			e = unix.Mkdirat(fd, part, 0755)
			if e == nil || e == unix.EEXIST {
				next, e = unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
				if e == nil {
					e = unix.Fchmod(next, 0755)
					if e != nil {
						unix.Close(next)
					}
				}
			}
		}
		unix.Close(fd)
		if e != nil {
			return nil, e
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), path), nil
}
func atomicWrite(path string, reader io.Reader, mode os.FileMode, uid, gid int) error {
	if err := checkPath(path); err != nil {
		return err
	}
	dir, err := openDir(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer dir.Close()
	var random [12]byte
	if _, err = rand.Read(random[:]); err != nil {
		return err
	}
	name := fmt.Sprintf(".molr-%x", random)
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(mode))
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	defer unix.Unlinkat(int(dir.Fd()), name, 0)
	if _, err = io.Copy(f, reader); err != nil {
		return err
	}
	if err = f.Chmod(mode); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		if err = f.Chown(uid, gid); err != nil {
			return err
		}
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = unix.Renameat(int(dir.Fd()), name, int(dir.Fd()), filepath.Base(path)); err != nil {
		return err
	}
	return dir.Sync()
}
func copyFile(to, from string, mode os.FileMode, uid, gid int) error {
	f, err := os.Open(from)
	if err != nil {
		return err
	}
	defer f.Close()
	return atomicWrite(to, f, mode, uid, gid)
}
func applyPermission(c *permissionChoice) error {
	d, err := openDir(c.path, false)
	if err != nil {
		return err
	}
	defer d.Close()
	info, err := d.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(info, c.info) || info.Mode() != c.info.Mode() || info.Sys().(*syscall.Stat_t).Uid != c.info.Sys().(*syscall.Stat_t).Uid || info.Sys().(*syscall.Stat_t).Gid != c.info.Sys().(*syscall.Stat_t).Gid {
		return fmt.Errorf("directory changed during confirmation: %s", c.path)
	}
	if c.action == "fix" {
		return d.Chmod(info.Mode() &^ 0022)
	}
	return nil
}
func lockInstance(program string) (*os.File, error) {
	dir, err := openDir(program, true)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	fd, err := unix.Openat(int(dir.Fd()), ".setup.lock", unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), ".setup.lock")
	info, err := f.Stat()
	if err == nil && (!info.Mode().IsRegular() || info.Sys().(*syscall.Stat_t).Nlink != 1 || info.Mode().Perm()&0077 != 0 || int(info.Sys().(*syscall.Stat_t).Uid) != os.Geteuid()) {
		err = fmt.Errorf("unsafe setup lock")
	}
	if err == nil {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
	}
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("cannot acquire installation lock (another operation may be running): %w", err)
	}
	return f, nil
}
func staticELF(path, arch string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("protected executable required: %s", path)
	}
	f, err := elf.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if f.Type != elf.ET_EXEC {
		return fmt.Errorf("static executable required: %s", path)
	}
	if arch != "" && ((arch == "amd64" && f.Machine != elf.EM_X86_64) || (arch == "arm64" && f.Machine != elf.EM_AARCH64)) {
		return fmt.Errorf("wrong executable architecture")
	}
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			return fmt.Errorf("dynamically linked executable: %s", path)
		}
	}
	libs, err := f.ImportedLibraries()
	if err != nil {
		return err
	}
	if len(libs) > 0 {
		return fmt.Errorf("dynamic dependencies in %s", path)
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
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		mount := fields[4]
		for _, v := range []string{"040", "011", "012", "134"} {
			n, _ := strconv.ParseInt(v, 8, 8)
			mount = strings.ReplaceAll(mount, "\\"+v, string(rune(n)))
		}
		if mount == path || strings.HasPrefix(mount, path+"/") {
			return fmt.Errorf("unmount filesystem before removal: %s", mount)
		}
	}
	return nil
}
func removeTree(path string) error {
	dir, err := openDir(filepath.Dir(path), false)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	// The /proc descriptor refers to the pinned directory, not a mutable pathname.
	root, err := os.OpenRoot(fmt.Sprintf("/proc/self/fd/%d", dir.Fd()))
	if err != nil {
		return err
	}
	defer root.Close()
	return root.RemoveAll(filepath.Base(path))
}
func removeFile(path string) error {
	if err := checkPath(path); err != nil {
		return err
	}
	dir, err := openDir(filepath.Dir(path), false)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	err = unix.Unlinkat(int(dir.Fd()), filepath.Base(path), 0)
	if err == unix.ENOENT {
		return nil
	}
	return err
}
