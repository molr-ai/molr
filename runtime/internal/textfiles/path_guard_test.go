package textfiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProtectedPathsRejectAliasesAndDescendants(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(root, "PrivateState")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(private, alias); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{private, filepath.Join(private, "future"), alias, filepath.Join(alias, "future")} {
		if !Overlaps(private, path) || !Overlaps(path, private) {
			t.Fatalf("protected alias accepted: %s", path)
		}
	}
	if Overlaps(private, private+"-other") || Overlaps("", private) {
		t.Fatal("unrelated or empty path overlaps")
	}
	upper := filepath.Join(root, strings.ToUpper(filepath.Base(private)))
	if _, err := os.Stat(upper); err == nil {
		if !Overlaps(private, upper) || !Overlaps(private, filepath.Join(upper, "future")) {
			t.Fatal("case alias exposed protected directory")
		}
	}
}
