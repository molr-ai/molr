package textfiles

import (
	"os"
	"path/filepath"
	"strings"
)

// Overlaps is a conservative deployment guard, not authorization to open a path.
// Compare existing directory identities as well as spelling: case-insensitive
// filesystems and system aliases must not expose Runtime state as a resource.
// Actual resource operations still require the confined, no-symlink file APIs.
func Overlaps(first, second string) bool {
	if first == "" || second == "" {
		return false
	}
	first, second = filepath.Clean(SystemPath(first)), filepath.Clean(SystemPath(second))
	beneath := func(root, name string) bool {
		relative, err := filepath.Rel(root, name)
		return err == nil && (relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
	}
	if beneath(first, second) || beneath(second, first) {
		return true
	}
	sameAncestor := func(root, name string) bool {
		info, err := os.Stat(root)
		if err != nil {
			return false
		}
		for {
			other, err := os.Stat(name)
			if err == nil && os.SameFile(info, other) {
				return true
			}
			parent := filepath.Dir(name)
			if parent == name {
				return false
			}
			name = parent
		}
	}
	return sameAncestor(first, second) || sameAncestor(second, first)
}
