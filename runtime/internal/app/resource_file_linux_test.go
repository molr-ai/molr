//go:build linux

package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
	"molr.ai/runtime/internal/core"
)

func TestResourceFileRejectsLinksAndSpecialFilesBeforeMutation(t *testing.T) {
	root := t.TempDir()
	need(t, os.Mkdir(filepath.Join(root, "allowed"), 0700))
	need(t, os.Mkdir(filepath.Join(root, "private"), 0700))
	secret := filepath.Join(root, "private/secret")
	need(t, os.WriteFile(secret, []byte("preserve"), 0600))
	need(t, os.Symlink("../private", filepath.Join(root, "allowed/link")))
	need(t, os.Link(secret, filepath.Join(root, "allowed/hard")))
	need(t, unix.Mkfifo(filepath.Join(root, "allowed/pipe"), 0600))
	for _, name := range []string{"allowed/link/secret", "allowed/hard", "allowed/pipe"} {
		if f, err := openResourceFile(root, name); err == nil {
			f.Close()
			t.Fatalf("unsafe read %s", name)
		}
		if _, err := replaceResourceFile(t.Context(), root, name, "bad", true, "", func() error { return nil }); err == nil {
			t.Fatalf("unsafe write %s", name)
		}
	}
	data, err := os.ReadFile(secret)
	need(t, err)
	if string(data) != "preserve" {
		t.Fatal("unsafe target modified")
	}
	if f, err := openResourceDirectory(root, "allowed/link"); err == nil {
		f.Close()
		t.Fatal("directory symlink followed")
	}
}
func TestResourceAtomicReplacementPreservesOriginalOnFailure(t *testing.T) {
	root := t.TempDir()
	name := "file"
	need(t, os.WriteFile(filepath.Join(root, name), []byte("original"), 0755))
	f, err := openResourceFile(root, name)
	need(t, err)
	version, err := resourceFileVersion(f)
	need(t, err)
	f.Close()
	cases := []struct {
		name      string
		ctx       context.Context
		authorize func() error
	}{
		{"revoked", t.Context(), func() error { return core.NewError(core.Forbidden, "revoked") }},
		{"external change", t.Context(), func() error { return os.WriteFile(filepath.Join(root, name), []byte("external"), 0755) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := replaceResourceFile(tc.ctx, root, name, "replacement", true, version, tc.authorize)
			if err == nil {
				t.Fatal("unsafe replacement committed")
			}
			data, err := os.ReadFile(filepath.Join(root, name))
			need(t, err)
			want := "original"
			if tc.name == "external change" {
				want = "external"
			}
			if string(data) != want {
				t.Fatalf("file partially changed: %q", data)
			}
			entries, err := os.ReadDir(root)
			need(t, err)
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".molr-write-") {
					t.Fatal("temporary write leaked")
				}
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = replaceResourceFile(ctx, root, "new", "no", false, "", func() error { return nil }); err == nil {
		t.Fatal("canceled write committed")
	}
	if _, err = os.Stat(filepath.Join(root, "new")); !os.IsNotExist(err) {
		t.Fatal("canceled create left file")
	}
}
