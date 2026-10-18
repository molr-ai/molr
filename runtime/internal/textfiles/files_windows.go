package textfiles

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"molr.ai/runtime/internal/core"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"
)

var commits sync.Mutex

func Supported() bool            { return true }
func SystemPath(s string) string { return s }
func Changed() error {
	return core.WithReason(core.NewError(core.Conflict, "file changed; read it again before editing"), "file_version_conflict")
}
func resourceOpenError(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return core.NewError(core.NotFound, "resource path does not exist")
	}
	return core.NewError(core.Forbidden, "resource path unavailable or cannot be confined")
}
func safeName(name string) bool {
	if !filepath.IsLocal(name) || strings.ContainsAny(name, ":\x00") {
		return false
	}
	for _, part := range strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' }) {
		if strings.HasSuffix(part, ".") && part != "." && part != ".." || strings.HasSuffix(part, " ") {
			return false
		}
	}
	return true
}

type confinedRoot struct {
	*os.Root
	pins []windows.Handle
}

func (r *confinedRoot) Close() error {
	err := r.Root.Close()
	for i := len(r.pins) - 1; i >= 0; i-- {
		windows.CloseHandle(r.pins[i])
	}
	return err
}
func openRoot(directory, name string) (*confinedRoot, error) {
	if !filepath.IsAbs(directory) || strings.HasPrefix(directory, `\\`) || strings.ContainsAny(strings.TrimPrefix(directory, filepath.VolumeName(directory)), ":\x00") || !safeName(name) {
		return nil, core.Invalid("path", "absolute root and confined relative path required")
	}
	currentPath := filepath.VolumeName(directory) + string(filepath.Separator)
	var pins []windows.Handle
	fail := func(err error) (*confinedRoot, error) {
		for _, h := range pins {
			windows.CloseHandle(h)
		}
		return nil, resourceOpenError(err)
	}
	for _, part := range append([]string{""}, strings.Split(strings.TrimPrefix(filepath.Clean(directory), currentPath), string(filepath.Separator))...) {
		currentPath = filepath.Join(currentPath, part)
		p, err := windows.UTF16PtrFromString(currentPath)
		if err != nil {
			return fail(err)
		}
		h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err != nil {
			return fail(err)
		}
		pins = append(pins, h)
		var info windows.ByHandleFileInformation
		if err = windows.GetFileInformationByHandle(h, &info); err != nil {
			return fail(err)
		}
		if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return fail(core.Invalid("path", "root reparse points prohibited"))
		}
	}
	opened, err := os.OpenRoot(directory)
	if err != nil {
		return fail(err)
	}
	root := &confinedRoot{Root: opened, pins: pins}
	// Reject reparse entries. os.Root independently enforces confinement if an
	// external process races a component replacement after this inspection.
	current := "."
	for _, part := range strings.FieldsFunc(filepath.Clean(name), func(r rune) bool { return r == '/' || r == '\\' }) {
		current = filepath.Join(current, part)
		st, e := root.Lstat(current)
		if errors.Is(e, os.ErrNotExist) {
			break
		}
		if e != nil || st.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			root.Close()
			return nil, resourceOpenError(e)
		}
	}
	return root, nil
}
func metadata(f *os.File) (windows.ByHandleFileInformation, error) {
	var st windows.ByHandleFileInformation
	err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &st)
	return st, err
}
func openEntry(directory, name string, dir bool) (*os.File, error) {
	root, err := openRoot(directory, name)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return openChecked(root, name, dir)
}
func openChecked(root *confinedRoot, name string, dir bool) (*os.File, error) {
	f, err := root.Open(name)
	if err != nil {
		return nil, resourceOpenError(err)
	}
	st, e := f.Stat()
	meta, me := metadata(f)
	if e != nil || me != nil || st.IsDir() != dir || (!dir && (!st.Mode().IsRegular() || meta.NumberOfLinks != 1)) || meta.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		f.Close()
		return nil, core.NewError(core.Forbidden, "ordinary single-link file or directory required")
	}
	return f, nil
}
func Open(directory, name string) (*os.File, error)          { return openEntry(directory, name, false) }
func OpenDirectory(directory, name string) (*os.File, error) { return openEntry(directory, name, true) }
func Version(f *os.File) (string, error) {
	st, err := metadata(f)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%d:%d:%d:%d:%d", st.VolumeSerialNumber, st.FileIndexHigh, st.FileIndexLow, st.FileSizeHigh, st.FileSizeLow, st.LastWriteTime.HighDateTime, st.LastWriteTime.LowDateTime)))
	return hex.EncodeToString(sum[:]), nil
}
func currentVersion(root *confinedRoot, name string, dir bool) (string, error) {
	f, e := openChecked(root, name, dir)
	if e != nil {
		return "", e
	}
	defer f.Close()
	return Version(f)
}
func Replace(ctx context.Context, directory, name, text string, overwrite bool, expected string, authorize func() error) (string, error) {
	if name == "." || !safeName(name) {
		return "", core.Invalid("path", "file path required")
	}
	if err := authorize(); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	commits.Lock()
	defer commits.Unlock()
	root, err := openRoot(directory, name)
	if err != nil {
		return "", err
	}
	defer root.Close()
	_, err = root.Lstat(name)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", resourceOpenError(err)
	}
	original := ""
	if exists {
		original, err = currentVersion(root, name, false)
		if err != nil {
			return "", err
		}
		if !overwrite {
			return "", core.WithReason(core.NewError(core.Conflict, "file exists; overwrite must be explicit"), "file_exists")
		}
	}
	if expected != "" && expected != original {
		return "", Changed()
	}
	token := make([]byte, 16)
	if _, err = rand.Read(token); err != nil {
		return "", err
	}
	temp := filepath.Join(filepath.Dir(name), ".molr-write-"+hex.EncodeToString(token))
	defer root.Remove(temp)
	out, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	_, err = out.WriteString(text)
	if err == nil {
		err = out.Sync()
	}
	out.Close()
	if err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if exists {
		now, e := currentVersion(root, name, false)
		if e != nil || now != original {
			return "", Changed()
		}
		err = root.Rename(temp, name)
	} else {
		err = root.Link(temp, name)
	}
	if err != nil {
		return "", resourceOpenError(err)
	}
	// Remove the temporary link before checking the final single-link invariant.
	if !exists {
		if err = root.Remove(temp); err != nil {
			return "", err
		}
	}
	return currentVersion(root, name, false)
}
func CreateOutput(directory, name string) (*os.File, error) {
	if filepath.Base(name) != name {
		return nil, core.Invalid("path", "output filename required")
	}
	root, err := openRoot(directory, name)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
}
func AppendOutput(directory, name string, offset int64, data []byte) error {
	commits.Lock()
	defer commits.Unlock()
	root, err := openRoot(directory, name)
	if err != nil {
		return err
	}
	defer root.Close()
	f, err := root.OpenFile(name, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := metadata(f)
	if err != nil {
		return err
	}
	if st.NumberOfLinks != 1 {
		return core.NewError(core.Forbidden, "single-link output required")
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() != offset {
		return Changed()
	}
	if _, err = f.WriteAt(data, offset); err != nil {
		return err
	}
	return f.Sync()
}
func MkdirAll(directory, name string) error {
	root, err := openRoot(directory, name)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.MkdirAll(name, 0700)
}
func MakeDirectory(ctx context.Context, directory, name string, authorize func() error) error {
	if name == "." {
		return core.Invalid("path", "child directory required")
	}
	if err := authorize(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := openRoot(directory, name)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.Mkdir(name, 0700)
}
func removeEntry(ctx context.Context, directory, name, expected string, dir bool, authorize func() error) error {
	if name == "." || expected == "" {
		return Changed()
	}
	if err := authorize(); err != nil {
		return err
	}
	commits.Lock()
	defer commits.Unlock()
	root, err := openRoot(directory, name)
	if err != nil {
		return err
	}
	defer root.Close()
	f, err := openChecked(root, name, dir)
	if err != nil {
		return err
	}
	defer f.Close()
	locked, err := lockEntry(f)
	if err != nil {
		return err
	}
	defer locked.Close()
	version, err := Version(locked)
	if err != nil {
		return err
	}
	if version != expected {
		return Changed()
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	remove := byte(1)
	return windows.SetFileInformationByHandle(windows.Handle(locked.Fd()), windows.FileDispositionInfo, &remove, 1)

}
func Remove(ctx context.Context, root, name, expected string, authorize func() error) error {
	return removeEntry(ctx, root, name, expected, false, authorize)
}
func RemoveDirectory(ctx context.Context, root, name, expected string, authorize func() error) error {
	return removeEntry(ctx, root, name, expected, true, authorize)
}

var reopenFile = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReOpenFile")

func lockEntry(f *os.File) (*os.File, error) {
	h, _, err := reopenFile.Call(f.Fd(), windows.GENERIC_READ|windows.DELETE, windows.FILE_SHARE_READ, windows.FILE_FLAG_BACKUP_SEMANTICS)
	if windows.Handle(h) == windows.InvalidHandle {
		return nil, err
	}
	return os.NewFile(h, "locked-entry"), nil
}

// Replace uses an optimistic version check and atomic replacement, like the Unix
// backend. Independent host writers do not participate in a filesystem CAS.
func Move(ctx context.Context, directory, from, to, expected string, dir bool, authorize func() error) (string, error) {
	if !safeName(from) || !safeName(to) || from == "." || to == "." || strings.EqualFold(from, to) || expected == "" {
		return "", core.Invalid("path", "distinct confined entries required")
	}
	if err := authorize(); err != nil {
		return "", err
	}
	commits.Lock()
	defer commits.Unlock()
	root, err := openRoot(directory, from)
	if err != nil {
		return "", err
	}
	defer root.Close()
	target, err := openRoot(directory, to)
	if err != nil {
		return "", err
	}
	defer target.Close()
	f, err := openChecked(root, from, dir)
	if err != nil {
		return "", err
	}
	defer f.Close()
	locked, err := lockEntry(f)
	if err != nil {
		return "", err
	}
	defer locked.Close()
	version, err := Version(locked)
	if err != nil {
		return "", err
	}
	if version != expected {
		return "", Changed()
	}
	parent, err := openChecked(target, filepath.Dir(to), true)
	if err != nil {
		return "", err
	}
	defer parent.Close()
	// Rename relative to the pinned destination directory and never replace an
	// existing target. Source deletion rights refer to the same verified object.
	type renameInfo struct {
		Flags  uint32
		Root   windows.Handle
		Length uint32
		Name   [1]uint16
	}
	name, err := windows.UTF16FromString(filepath.Base(to))
	if err != nil {
		return "", err
	}
	name = name[:len(name)-1]
	offset := unsafe.Offsetof(renameInfo{}.Name)
	buffer := make([]byte, int(offset)+len(name)*2)
	info := (*renameInfo)(unsafe.Pointer(&buffer[0]))
	info.Root = windows.Handle(parent.Fd())
	info.Length = uint32(len(name) * 2)
	copy(unsafe.Slice(&info.Name[0], len(name)), name)
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = windows.SetFileInformationByHandle(windows.Handle(locked.Fd()), windows.FileRenameInfo, &buffer[0], uint32(len(buffer))); err != nil {
		return "", resourceOpenError(err)
	}
	return Version(locked)
}
