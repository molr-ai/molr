//go:build linux

package textfiles

import (
	"golang.org/x/sys/unix"
	"molr.ai/runtime/internal/core"
	"os"
	"path/filepath"
	"strings"
)

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
func openConfined(fd int, name string, flags int) (int, error) {
	return unix.Openat2(fd, name, &unix.OpenHow{Flags: uint64(flags), Resolve: resourceResolve})
}
func renameEntry(fromFD int, from string, toFD int, to string, exclusive bool) error {
	var flags uint
	if exclusive {
		flags = unix.RENAME_NOREPLACE
	}
	return unix.Renameat2(fromFD, from, toFD, to, flags)
}

func Supported() bool { return true }

// SystemPath leaves native paths unchanged on this platform.
func SystemPath(directory string) string { return directory }
