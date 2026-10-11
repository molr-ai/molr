//go:build linux

package textfiles

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
	"molr.ai/runtime/internal/core"
)

// Serialize the final local filesystem check and commit across all Runtime APIs.
// Authorization runs before this short critical section; external writers do not participate.
var commits sync.Mutex

const resourceResolve = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS

func OpenRoot(directory string) (*os.File, error) {
	if directory == "/" {
		return os.Open("/")
	}
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || directory == "/" {
		return nil, core.Invalid("path", "dedicated absolute resource root required")
	}
	base, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, core.NewError(core.Unavailable, "resource root unavailable")
	}
	defer unix.Close(base)
	fd, err := unix.Openat2(base, strings.TrimPrefix(directory, "/"), &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: resourceResolve})
	if err != nil {
		return nil, resourceOpenError(err)
	}
	return os.NewFile(uintptr(fd), "resource-root"), nil
}
func resourceOpenError(err error) error {
	if errors.Is(err, unix.ENOENT) {
		return core.NewError(core.NotFound, "resource path does not exist")
	}
	return core.NewError(core.Forbidden, "resource path is unavailable, contains links, or cannot be confined")
}
func openResourceEntry(directory, name string, dir bool) (*os.File, error) {
	if directory == "/" {
		f, err := os.OpenFile(filepath.Join(directory, name), os.O_RDONLY|unix.O_NONBLOCK, 0)
		if err != nil {
			return nil, resourceOpenError(err)
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, err
		}
		if (dir && !st.IsDir()) || (!dir && !st.Mode().IsRegular()) {
			f.Close()
			return nil, core.Invalid("path", "regular file or directory required")
		}
		return f, nil
	}

	if !filepath.IsLocal(name) || strings.ContainsAny(name, "\x00\\") {
		return nil, core.Invalid("path", "confined resource path required")
	}
	root, err := OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	flags := uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK)
	if dir {
		flags |= unix.O_DIRECTORY
	}
	fd, err := unix.Openat2(int(root.Fd()), name, &unix.OpenHow{Flags: flags, Resolve: resourceResolve})
	if err != nil {
		return nil, resourceOpenError(err)
	}
	f := os.NewFile(uintptr(fd), "resource-entry")
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		f.Close()
		return nil, core.NewError(core.Unavailable, "resource metadata unavailable")
	}
	if !dir && (st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1) {
		f.Close()
		return nil, core.NewError(core.Forbidden, "single-link regular file required")
	}
	return f, nil
}
func Open(directory, name string) (*os.File, error) {
	return openResourceEntry(directory, name, false)
}
func OpenDirectory(directory, name string) (*os.File, error) {
	return openResourceEntry(directory, name, true)
}
func statVersion(st unix.Stat_t) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%d:%d:%d:%d:%d", st.Dev, st.Ino, st.Size, st.Mtim.Sec, st.Mtim.Nsec, st.Ctim.Sec, st.Ctim.Nsec)))
	return hex.EncodeToString(sum[:])
}
func Version(f *os.File) (string, error) {
	var st unix.Stat_t
	if unix.Fstat(int(f.Fd()), &st) != nil {
		return "", core.NewError(core.Unavailable, "resource metadata unavailable")
	}
	return statVersion(st), nil
}
func Changed() error {
	return core.WithReason(core.NewError(core.Conflict, "file changed; read it again before editing"), "file_version_conflict")
}

// Commit within a pinned no-symlink parent. Existing files are never truncated;
// readers see the old complete file or the new complete file. Our caller
// serializes local tool mutations; external writers are checked before commit,
// but arbitrary external processes are not participants in a filesystem CAS.
func Replace(ctx context.Context, directory, name, text string, overwrite bool, expected string, authorize func() error) (string, error) {
	if !filepath.IsLocal(name) || strings.ContainsAny(name, "\x00\\") || name == "." {
		return "", core.Invalid("path", "confined file path required")
	}
	parentName, base := filepath.Dir(name), filepath.Base(name)
	parent, err := OpenDirectory(directory, parentName)
	if err != nil {
		return "", err
	}
	defer parent.Close()
	fd := int(parent.Fd())
	var original unix.Stat_t
	statErr := unix.Fstatat(fd, base, &original, unix.AT_SYMLINK_NOFOLLOW)
	exists := statErr == nil
	if statErr != nil && !errors.Is(statErr, unix.ENOENT) {
		return "", resourceOpenError(statErr)
	}
	if exists && (original.Mode&unix.S_IFMT != unix.S_IFREG || (directory != "/" && original.Nlink != 1)) {
		return "", core.NewError(core.Forbidden, "single-link regular file required")
	}
	if exists && !overwrite {
		return "", core.WithReason(core.NewError(core.Conflict, "file exists; overwrite must be explicit"), "file_exists")
	}
	if expected != "" && (!exists || statVersion(original) != expected) {
		return "", Changed()
	}
	token := make([]byte, 16)
	if _, err = rand.Read(token); err != nil {
		return "", core.NewError(core.Unavailable, "cannot prepare file write")
	}
	temporary := ".molr-write-" + hex.EncodeToString(token)
	outFD, err := unix.Openat(fd, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return "", core.NewError(core.Unavailable, "cannot prepare file write")
	}
	defer unix.Unlinkat(fd, temporary, 0)
	out := os.NewFile(uintptr(outFD), "resource-write")
	defer out.Close()
	if exists {
		if unix.Fchmod(outFD, original.Mode&0777) != nil {
			return "", core.NewError(core.Unavailable, "cannot preserve file mode")
		}
	}
	if _, err = out.WriteString(text); err != nil {
		return "", core.NewError(core.Unavailable, "file write failed before commit")
	}
	if err = out.Sync(); err != nil {
		return "", core.NewError(core.Unavailable, "file write failed before commit")
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = authorize(); err != nil {
		return "", err
	}
	commits.Lock()
	defer commits.Unlock()
	// Refuse a renamed/rebound directory rather than modifying an old path alias.
	currentParent, err := OpenDirectory(directory, parentName)
	if err != nil {
		return "", err
	}
	var before, after unix.Stat_t
	aerr := unix.Fstat(fd, &before)
	berr := unix.Fstat(int(currentParent.Fd()), &after)
	currentParent.Close()
	if aerr != nil || berr != nil || before.Dev != after.Dev || before.Ino != after.Ino {
		return "", Changed()
	}
	var current unix.Stat_t
	err = unix.Fstatat(fd, base, &current, unix.AT_SYMLINK_NOFOLLOW)
	if exists {
		if err != nil || current.Mode&unix.S_IFMT != unix.S_IFREG || (directory != "/" && current.Nlink != 1) || statVersion(current) != statVersion(original) {
			return "", Changed()
		}
	} else if !errors.Is(err, unix.ENOENT) {
		return "", Changed()
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	flags := uint(0)
	if !exists {
		flags = unix.RENAME_NOREPLACE
	}
	if err = unix.Renameat2(fd, temporary, fd, base, flags); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return "", Changed()
		}
		return "", core.NewError(core.Unavailable, "atomic file replacement failed")
	}
	if err = parent.Sync(); err != nil {
		return "", core.NewError(core.Unavailable, "file committed but directory sync failed")
	}
	return Version(out)
}

// CreateOutput opens a fresh pinned workspace file. No existing file or
// symbolic/hard link can be overwritten by runtime output capture.
func CreateOutput(directory, name string) (*os.File, error) {
	if filepath.Base(name) != name {
		return nil, core.Invalid("path", "output filename required")
	}
	root, err := OpenDirectory(directory, ".")
	if err != nil {
		return nil, err
	}
	defer root.Close()
	fd, err := unix.Openat(int(root.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, resourceOpenError(err)
	}
	return os.NewFile(uintptr(fd), name), nil
}

// AppendOutput uses the same confined parent and link checks as file tools.
// The caller serializes Runtime writes; an externally changed length is a conflict.
func AppendOutput(directory, name string, offset int64, data []byte) error {
	commits.Lock()
	defer commits.Unlock()
	if filepath.Base(name) != name {
		return core.Invalid("path", "output filename required")
	}
	root, err := OpenDirectory(directory, ".")
	if err != nil {
		return err
	}
	defer root.Close()
	fd, err := unix.Openat(int(root.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return resourceOpenError(err)
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 {
		return core.NewError(core.Forbidden, "ordinary unlinked output file required")
	}
	if st.Size != offset {
		return Changed()
	}
	if _, err = f.WriteAt(data, offset); err != nil {
		return err
	}
	return f.Sync()
}

// MkdirAll creates descendants through pinned descriptors, without following links.
func MkdirAll(root, name string) error {
	if !filepath.IsLocal(name) || strings.ContainsAny(name, "\x00\\") {
		return core.Invalid("path", "confined directory required")
	}
	current, err := OpenDirectory(root, ".")
	if err != nil {
		return err
	}
	defer func() { current.Close() }()
	for _, part := range strings.Split(filepath.Clean(name), string(filepath.Separator)) {
		if part == "." {
			continue
		}
		if err = unix.Mkdirat(int(current.Fd()), part, 0700); err != nil && !errors.Is(err, unix.EEXIST) {
			return resourceOpenError(err)
		}
		fd, err := unix.Openat(int(current.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return resourceOpenError(err)
		}
		current.Close()
		current = os.NewFile(uintptr(fd), "memory-directory")
	}
	return current.Sync()
}

func Remove(ctx context.Context, root, name, expected string, authorize func() error) error {
	if !filepath.IsLocal(name) || name == "." || strings.ContainsAny(name, "\x00\\") {
		return core.Invalid("path", "confined file required")
	}
	parent, err := OpenDirectory(root, filepath.Dir(name))
	if err != nil {
		return err
	}
	defer parent.Close()
	var st unix.Stat_t
	if err = unix.Fstatat(int(parent.Fd()), filepath.Base(name), &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return resourceOpenError(err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 {
		return core.NewError(core.Forbidden, "single-link regular file required")
	}
	if expected == "" || statVersion(st) != expected {
		return Changed()
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = authorize(); err != nil {
		return err
	}
	commits.Lock()
	defer commits.Unlock()
	again, err := OpenDirectory(root, filepath.Dir(name))
	if err != nil {
		return err
	}
	defer again.Close()
	a, _ := parent.Stat()
	b, _ := again.Stat()
	if a == nil || b == nil || !os.SameFile(a, b) {
		return Changed()
	}
	var current unix.Stat_t
	if err = unix.Fstatat(int(parent.Fd()), filepath.Base(name), &current, unix.AT_SYMLINK_NOFOLLOW); err != nil || statVersion(current) != expected {
		return Changed()
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = unix.Unlinkat(int(parent.Fd()), filepath.Base(name), 0); err != nil {
		return err
	}
	return parent.Sync()
}

// Move renames one confined file or directory without replacing a destination.
func Move(ctx context.Context, root, from, to, expected string, directory bool, authorize func() error) (string, error) {
	for _, name := range []string{from, to} {
		if !filepath.IsLocal(name) || filepath.Clean(name) != name || name == "." || strings.ContainsAny(name, "\x00\\") {
			return "", core.Invalid("path", "confined canonical path required")
		}
	}
	if from == to || strings.HasPrefix(to, from+"/") {
		return "", core.Invalid("path", "distinct destination outside source required")
	}
	source, err := OpenDirectory(root, filepath.Dir(from))
	if err != nil {
		return "", err
	}
	defer source.Close()
	dest, err := OpenDirectory(root, filepath.Dir(to))
	if err != nil {
		return "", err
	}
	defer dest.Close()
	var original unix.Stat_t
	if err = unix.Fstatat(int(source.Fd()), filepath.Base(from), &original, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return "", resourceOpenError(err)
	}
	kind := uint32(unix.S_IFREG)
	if directory {
		kind = unix.S_IFDIR
	}
	if original.Mode&unix.S_IFMT != kind || !directory && original.Nlink != 1 {
		return "", core.NewError(core.Forbidden, "ordinary file or directory required")
	}
	if expected == "" || statVersion(original) != expected {
		return "", Changed()
	}
	if err = authorize(); err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	commits.Lock()
	defer commits.Unlock()
	for _, entry := range []struct {
		file *os.File
		name string
	}{{source, filepath.Dir(from)}, {dest, filepath.Dir(to)}} {
		current, err := OpenDirectory(root, entry.name)
		if err != nil {
			return "", err
		}
		a, _ := entry.file.Stat()
		b, _ := current.Stat()
		current.Close()
		if a == nil || b == nil || !os.SameFile(a, b) {
			return "", Changed()
		}
	}
	var current unix.Stat_t
	if err = unix.Fstatat(int(source.Fd()), filepath.Base(from), &current, unix.AT_SYMLINK_NOFOLLOW); err != nil || statVersion(current) != expected {
		return "", Changed()
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = unix.Renameat2(int(source.Fd()), filepath.Base(from), int(dest.Fd()), filepath.Base(to), unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.EEXIST) || errors.Is(err, unix.ENOTEMPTY) {
			return "", Changed()
		}
		return "", resourceOpenError(err)
	}
	if err = source.Sync(); err != nil {
		return "", err
	}
	if err = dest.Sync(); err != nil {
		return "", err
	}
	if err = unix.Fstatat(int(dest.Fd()), filepath.Base(to), &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return "", err
	}
	return statVersion(current), nil
}
func RemoveDirectory(ctx context.Context, root, name, expected string, authorize func() error) error {
	if !filepath.IsLocal(name) || filepath.Clean(name) != name || name == "." {
		return core.Invalid("path", "confined directory required")
	}
	dir, err := OpenDirectory(root, name)
	if err != nil {
		return err
	}
	defer dir.Close()
	version, err := Version(dir)
	if err != nil {
		return err
	}
	if version != expected {
		return Changed()
	}
	parent, err := OpenDirectory(root, filepath.Dir(name))
	if err != nil {
		return err
	}
	defer parent.Close()
	if err = authorize(); err != nil {
		return err
	}
	commits.Lock()
	defer commits.Unlock()
	again, err := OpenDirectory(root, filepath.Dir(name))
	if err != nil {
		return err
	}
	a, _ := parent.Stat()
	b, _ := again.Stat()
	again.Close()
	if a == nil || b == nil || !os.SameFile(a, b) {
		return Changed()
	}
	var st unix.Stat_t
	if err = unix.Fstatat(int(parent.Fd()), filepath.Base(name), &st, unix.AT_SYMLINK_NOFOLLOW); err != nil || statVersion(st) != expected {
		return Changed()
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = unix.Unlinkat(int(parent.Fd()), filepath.Base(name), unix.AT_REMOVEDIR); err != nil {
		if errors.Is(err, unix.ENOTEMPTY) {
			return core.NewError(core.Conflict, "directory is not empty")
		}
		return resourceOpenError(err)
	}
	return parent.Sync()
}
