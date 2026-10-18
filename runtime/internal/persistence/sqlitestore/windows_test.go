//go:build windows

package sqlitestore

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsFileURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "中文 file.db")
	value := FileURL(path)
	if !strings.HasPrefix(value, "file:///") || strings.Contains(value, "%5C") {
		t.Fatal(value)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
}
