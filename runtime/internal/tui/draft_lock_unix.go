//go:build !windows

package tui

import (
	"golang.org/x/sys/unix"
	"os"
)

func lockDraftFile(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) }
