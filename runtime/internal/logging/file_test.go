package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newTestFile(t *testing.T, path string, options Options) *File {
	t.Helper()
	f, err := NewFile(path, options)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func writeRecord(t *testing.T, f *File, record string) {
	t.Helper()
	n, err := f.Write([]byte(record))
	if err != nil || n != len(record) {
		t.Fatalf("write: %d, %v", n, err)
	}
}

func TestFileRotationRetainsWholeRecordsAndDeletesOldest(t *testing.T) {
	for _, backups := range []int{0, 1, 3} {
		t.Run(fmt.Sprint(backups), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "private", "runtime.log")
			f := newTestFile(t, path, Options{MaxFileBytes: 4, BackupFiles: backups})
			if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
				t.Fatal("lazy constructor touched disk")
			}
			for i := range 8 {
				writeRecord(t, f, fmt.Sprintf("%03d\n", i))
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(entries) != backups+1 {
				t.Fatalf("retention: %v, %v", entries, err)
			}
			for i := range backups + 1 {
				name := path
				if i > 0 {
					name += fmt.Sprintf(".%d", i)
				}
				data, err := os.ReadFile(name)
				if err != nil || string(data) != fmt.Sprintf("%03d\n", 7-i) {
					t.Fatalf("archive order: %q, %v", data, err)
				}
				info, err := os.Stat(name)
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatal("private file permissions", err)
				}
			}
		})
	}
}

func TestConcurrentWritesStayIntactAcrossRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.log")
	f := newTestFile(t, path, Options{MaxFileBytes: 40, BackupFiles: 9})
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Go(func() { writeRecord(t, f, fmt.Sprintf("%03d\n", i)) })
	}
	wg.Wait()
	seen := map[string]bool{}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 10 {
		t.Fatal("concurrent rotation lost files", err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(filepath.Dir(path), entry.Name()))
		if err != nil || len(data) != 40 {
			t.Fatal("record split or file exceeded limit", err)
		}
		for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
			if len(line) != 3 || seen[line] {
				t.Fatal("corrupt or duplicated record", line)
			}
			seen[line] = true
		}
	}
	if len(seen) != 100 {
		t.Fatal("lost records", len(seen))
	}
}

func TestRestartAppliesReducedRetentionAndPreservesOtherFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.log")
	f := newTestFile(t, path, Options{MaxFileBytes: 8, BackupFiles: 3})
	for range 4 {
		writeRecord(t, f, "old-data")
	}
	for _, suffix := range []string{".notes", ".01", ".0"} {
		if err := os.WriteFile(path+suffix, []byte("unrelated"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	f, err := OpenFile(path, Options{MaxFileBytes: 4, BackupFiles: 1})
	if err != nil {
		t.Fatal(err)
	}
	writeRecord(t, f, "new\n")
	for _, suffix := range []string{".1", ".2", ".3"} {
		if _, err := os.Stat(path + suffix); !os.IsNotExist(err) {
			t.Fatal("obsolete or oversized backup remains", suffix)
		}
	}
	for _, suffix := range []string{".notes", ".01", ".0"} {
		if data, err := os.ReadFile(path + suffix); err != nil || string(data) != "unrelated" {
			t.Fatal("unrelated file modified", suffix)
		}
	}
}

func TestOversizedRecordAndInvalidDestinationDoNotDestroyLogs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.log")
	f := newTestFile(t, path, Options{MaxFileBytes: 4, BackupFiles: 1})
	writeRecord(t, f, "old\n")
	if n, err := f.Write([]byte("too large")); n != 0 || err == nil {
		t.Fatal("accepted oversized record")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "old\n" {
		t.Fatal("oversized record destroyed log")
	}
	if err := os.Mkdir(path+".1", 0700); err != nil {
		t.Fatal(err)
	}
	if n, err := f.Write([]byte("new\n")); n != 0 || err == nil {
		t.Fatal("rotation accepted directory as backup")
	}
	if data, _ := os.ReadFile(path); string(data) != "old\n" {
		t.Fatal("failed rotation destroyed current log")
	}
	if err := os.Remove(path + ".1"); err != nil {
		t.Fatal(err)
	}
	writeRecord(t, f, "new\n")
}

func TestFileRejectsSymlinks(t *testing.T) {
	dir := t.TempDir()
	target, path := filepath.Join(dir, "target"), filepath.Join(dir, "runtime.log")
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skip(err)
	}
	if _, err := OpenFile(path, DefaultOptions()); err == nil {
		t.Fatal("followed symlink")
	}
	if data, _ := os.ReadFile(target); string(data) != "keep" {
		t.Fatal("modified symlink target")
	}
}

func TestInvalidPolicyHasNoFilesystemEffects(t *testing.T) {
	for _, options := range []Options{{}, {MaxFileBytes: -1, BackupFiles: 1}, {MaxFileBytes: 1, BackupFiles: -1}, {MaxFileBytes: 1, BackupFiles: 101}} {
		path := filepath.Join(t.TempDir(), "runtime.log")
		if _, err := OpenFile(path, options); err == nil {
			t.Fatal("invalid policy accepted")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("invalid policy touched disk")
		}
	}
}
