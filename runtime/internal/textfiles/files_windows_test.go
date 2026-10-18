package textfiles

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsConfinedFiles(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	allow := func() error { return nil }
	for _, name := range []string{"../outside", `C:\Windows\win.ini`, "file:stream", "NUL", "COM1", "trailing. ", `\\server\share\file`} {
		if _, err := Open(root, name); err == nil {
			t.Fatal("unsafe path accepted", name)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(outside, "secret"), filepath.Join(root, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root, "hardlink"); err == nil {
		t.Fatal("hard link accepted")
	}
	if err := os.Symlink(outside, filepath.Join(root, "outside")); err == nil {
		if _, err := Open(root, "outside/secret"); err == nil {
			t.Fatal("external symlink accepted")
		}
	}
	first, err := Replace(t.Context(), root, "中文.txt", "hello", false, "", allow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Replace(t.Context(), root, "中文.txt", "bad", true, "stale", allow); err == nil {
		t.Fatal("stale version accepted")
	}
	next, err := Replace(t.Context(), root, "中文.txt", "updated", true, first, allow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Replace(t.Context(), root, "occupied.txt", "keep", false, "", allow); err != nil {
		t.Fatal(err)
	}
	if _, err = Move(t.Context(), root, "中文.txt", "occupied.txt", next, false, allow); err == nil {
		t.Fatal("move replaced destination")
	}
	next, err = Move(t.Context(), root, "中文.txt", "moved.txt", next, false, allow)
	if err != nil {
		t.Fatal(err)
	}
	if err = Remove(context.Background(), root, "moved.txt", next, allow); err != nil {
		t.Fatal(err)
	}
	f, err := CreateOutput(root, "output.log")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err = AppendOutput(root, "output.log", 0, []byte("stream")); err != nil {
		t.Fatal(err)
	}
	if err = AppendOutput(root, "output.log", 0, []byte("overwrite")); err == nil {
		t.Fatal("stale output offset accepted")
	}
}

func TestWindowsRootPinnedDuringMutation(t *testing.T) {
	root := t.TempDir()
	handle, err := openRoot(root, ".")
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	if err = os.Rename(root, root+"-replaced"); err == nil {
		t.Fatal("resource root replaced during pinned operation")
	}
	for _, bad := range []string{`\\server\share`, `\\?\C:\`, `\\.\C:\`} {
		if r, err := openRoot(bad, "."); err == nil {
			r.Close()
			t.Fatal("device/UNC root accepted")
		}
	}
}
