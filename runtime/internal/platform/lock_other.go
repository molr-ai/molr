//go:build !unix && !windows

package platform

import (
	"errors"
	"os"
)

func lockFile(f *os.File) error   { return errors.New("instance locking unsupported on this platform") }
func unlockFile(f *os.File) error { return nil }
