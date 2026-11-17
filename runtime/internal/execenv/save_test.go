package execenv

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveSharesLockAndPreservesUpdates(t *testing.T) {
	directory := privateDirectory(t)
	if err := Save(t.Context(), directory, map[string]string{"PATH": directory, "CUSTOM": "retained"}); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	owner, err := root.Stat(".")
	if err != nil {
		t.Fatal(err)
	}
	lock, err := openLock(root, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := lockFile(lock); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(directory, Filename))
	if err := Save(t.Context(), directory, map[string]string{"PATH": filepath.Join(directory, "second")}); err == nil {
		t.Fatal("concurrent save ignored lock")
	}
	after, _ := os.ReadFile(filepath.Join(directory, Filename))
	if !bytes.Equal(before, after) {
		t.Fatal("blocked writer changed environment")
	}
	unlockFile(lock)
	lock.Close()
	if err := Save(t.Context(), directory, map[string]string{"PATH": filepath.Join(directory, "second")}); err != nil {
		t.Fatal(err)
	}
	values, err := Read(directory)
	if err != nil || values["PATH"] != filepath.Join(directory, "second") || values["CUSTOM"] != "retained" {
		t.Fatal(values, err)
	}
	for _, name := range []string{Filename, lockName} {
		info, err := os.Stat(filepath.Join(directory, name))
		if err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
			t.Fatal(info, err)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 2 {
		t.Fatal("temporary write leaked", entries, err)
	}
}

func TestSaveRejectsCanceledInvalidAndLinkedTargets(t *testing.T) {
	directory := privateDirectory(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Save(ctx, directory, map[string]string{"PATH": directory}); err != context.Canceled {
		t.Fatal(err)
	}
	if err := Save(t.Context(), directory, map[string]string{"PATH": "relative"}); err == nil {
		t.Fatal("invalid PATH accepted")
	}
	entries, _ := os.ReadDir(directory)
	if len(entries) != 0 {
		t.Fatal("invalid save wrote files")
	}
	if runtime.GOOS == "windows" {
		return
	}
	target := filepath.Join(directory, "untouched")
	if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, link := range []func(string, string) error{os.Symlink, os.Link} {
		for _, name := range []string{lockName, Filename} {
			if err := link(target, filepath.Join(directory, name)); err != nil {
				t.Fatal(err)
			}
			if err := Save(t.Context(), directory, map[string]string{"PATH": directory}); err == nil {
				t.Fatal("linked target accepted", name)
			}
			os.Remove(filepath.Join(directory, name))
			if data, _ := os.ReadFile(target); string(data) != "unchanged" {
				t.Fatal("linked file changed")
			}
		}
		os.Remove(filepath.Join(directory, lockName))
	}
}
