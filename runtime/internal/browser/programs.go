package browser

import (
	"path/filepath"
	"runtime"
)

// ChromiumExecutable preserves the signed macOS application bundle layout.
func ChromiumExecutable(directory string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(directory, "Google Chrome for Testing.app", "Contents", "MacOS", "Google Chrome for Testing")
	}
	return filepath.Join(directory, "chrome")
}
