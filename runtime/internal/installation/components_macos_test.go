//go:build linux || darwin

package installation

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacBrowserBundleLinks(t *testing.T) {
	for _, target := range []string{"Versions/A", "../../outside", "/etc", "missing", "Framework"} {
		t.Run(strings.ReplaceAll(target, "/", "_"), func(t *testing.T) {
			root := t.TempDir()
			root, _ = filepath.EvalSymlinks(root)
			archive := filepath.Join(root, "bundle.zip")
			f, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			z := zip.NewWriter(f)
			for _, entry := range []struct {
				name, data string
				mode       os.FileMode
			}{
				{"bundle/chrome", "executable", 0755},
				{"bundle/Versions/A/library", "signed bytes", 0644},
				{"bundle/Framework", target, os.ModeSymlink | 0777},
			} {
				h := &zip.FileHeader{Name: entry.name}
				h.SetMode(entry.mode)
				w, err := z.CreateHeader(h)
				if err != nil {
					t.Fatal(err)
				}
				w.Write([]byte(entry.data))
			}
			z.Close()
			f.Close()
			destination := filepath.Join(root, "out")
			os.Mkdir(destination, 0700)
			err = extractComponent(archive, destination, componentAsset{Prefix: "bundle/", Executable: "chrome", Expanded: 4096, BundleLinks: true})
			if (err == nil) != (target == "Versions/A") {
				t.Fatalf("%q: %v", target, err)
			}
			if err == nil {
				data, err := os.ReadFile(filepath.Join(destination, "Framework/library"))
				if err != nil || string(data) != "signed bytes" {
					t.Fatalf("%s %v", data, err)
				}
			}
		})
	}
}

// Opt-in verifies actual pinned archives, including every framework symlink and
// unchanged codesigned files, without downloading during ordinary unit tests.
func TestMacBrowserRealArchive(t *testing.T) {
	archive := os.Getenv("MOLR_TEST_BROWSER_ARCHIVE")
	if archive == "" {
		t.Skip("pinned archive not configured")
	}
	arch := os.Getenv("MOLR_TEST_BROWSER_ARCH")
	if arch == "" {
		arch = "arm64"
	}
	release, err := darwinBrowserRelease(arch)
	if err != nil {
		t.Fatal(err)
	}
	destination, _ := filepath.EvalSymlinks(t.TempDir())
	if err = extractComponent(archive, destination, release.Assets[1]); err != nil {
		t.Fatal(err)
	}
}
