package textfiles

import (
	"golang.org/x/sys/unix"
	"molr.ai/runtime/internal/core"
	"os"
	"path/filepath"
	"strings"
)

// O_NOFOLLOW_ANY makes the kernel reject links in every path component in a
// single lookup. Names below a pinned root are cleaned and cannot contain .. .
func openConfined(fd int, name string, flags int) (int, error) {
	return unix.Openat(fd, filepath.Clean(name), flags|unix.O_NOFOLLOW_ANY, 0)
}
func OpenRoot(directory string) (*os.File, error) {
	if directory == "/" {
		return os.Open("/")
	}
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return nil, core.Invalid("path", "dedicated absolute resource root required")
	}
	directory = SystemPath(directory)
	fd, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW_ANY, 0)
	if err != nil {
		return nil, resourceOpenError(err)
	}
	return os.NewFile(uintptr(fd), "resource-root"), nil
}
func renameEntry(fromFD int, from string, toFD int, to string, exclusive bool) error {
	if exclusive {
		return unix.RenameatxNp(fromFD, from, toFD, to, unix.RENAME_EXCL)
	}
	return unix.Renameat(fromFD, from, toFD, to)
}
func Supported() bool { return true }

// SystemPath expands only verified macOS system directory aliases.
func SystemPath(directory string) string {
	// macOS provides these system aliases. Resolve only the known system link,
	// never arbitrary links inside a configured resource or workspace.
	for _, alias := range []string{"/tmp", "/var", "/etc"} {
		if directory == alias || strings.HasPrefix(directory, alias+"/") {
			target, err := os.Readlink(alias)
			if err == nil && target == "private"+alias {
				directory = "/private" + directory
			}
		}
	}
	return directory
}
