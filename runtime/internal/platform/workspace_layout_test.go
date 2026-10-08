//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceLayoutPreservesFilesAndRejectsLinks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "assistant")
	if err := EnsureWorkspace(root); err != nil {
		t.Fatal(err)
	}
	for _, name := range WorkspaceDirectories() {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
			t.Fatalf("directory %s: %v %v", name, info, err)
		}
	}
	for _, name := range []string{".agents/runtime", ".config", ".local/share"} {
		if _, err := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("unexpected directory %s: %v", name, err)
		}
	}
	target := filepath.Join(root, "outputs/report.txt")
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureWorkspace(root); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(target)
	if err != nil || string(body) != "keep" {
		t.Fatal("initialization replaced a user file")
	}
	for _, name := range []string{"inbox", ".agents", ".local", ".local/state", ".local/state/molr"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, name)
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			if err := os.Symlink(outside, target); err != nil {
				t.Fatal(err)
			}
			if err := EnsureWorkspace(root); err == nil {
				t.Fatal("followed layout symlink")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatal("wrote outside workspace")
			}
		})
	}
}
