//go:build linux || darwin

package textfiles

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"molr.ai/runtime/internal/core"
)

func TestPinnedFileLifecycleAndVersions(t *testing.T) {
	root := t.TempDir()
	ctx := t.Context()
	allow := func() error { return nil }
	if err := MkdirAll(root, "a/b"); err != nil {
		t.Fatal(err)
	}
	dir, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	dir.Close()
	dir, err = OpenDirectory(root, "a/b")
	if err != nil {
		t.Fatal(err)
	}
	dir.Close()
	version, err := Replace(ctx, root, "a/b/note.md", "first", false, "", allow)
	if err != nil {
		t.Fatal(err)
	}
	f, err := Open(root, "a/b/note.md")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Version(f)
	b, e := io.ReadAll(f)
	f.Close()
	if err != nil || e != nil || got != version || string(b) != "first" {
		t.Fatal(string(b), err, e)
	}
	_, err = Replace(ctx, root, "a/b/note.md", "second", true, "stale", allow)
	if core.CodeOf(err) != core.Conflict {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = Replace(canceled, root, "a/b/note.md", "second", true, version, allow)
	if err == nil {
		t.Fatal("canceled write committed")
	}
	next, err := Replace(ctx, root, "a/b/note.md", "second", true, version, allow)
	if err != nil || next == version {
		t.Fatal(next, err)
	}
	if err = Remove(ctx, root, "a/b/note.md", version, allow); core.CodeOf(err) != core.Conflict {
		t.Fatal(err)
	}
	if err = Remove(ctx, root, "a/b/note.md", next, allow); err != nil {
		t.Fatal(err)
	}
	out, err := CreateOutput(root, "output")
	if err != nil {
		t.Fatal(err)
	}
	out.Close()
	if err = AppendOutput(root, "output", 0, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err = AppendOutput(root, "output", 0, []byte("two")); core.CodeOf(err) != core.Conflict {
		t.Fatal(err)
	}
	if core.CodeOf(Changed()) != core.Conflict {
		t.Fatal("missing conflict code")
	}
}
func TestPinnedFileRejectsLinksAndParentRebind(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := MkdirAll(root, "link/child"); err == nil {
		t.Fatal("followed directory link")
	}
	if _, err := OpenDirectory(root, "link"); err == nil {
		t.Fatal("opened directory link")
	}
	if err := MkdirAll(root, "safe"); err != nil {
		t.Fatal(err)
	}
	_, err := Replace(t.Context(), root, "safe/note", "content", false, "", func() error {
		if err := os.Rename(filepath.Join(root, "safe"), filepath.Join(root, "moved")); err != nil {
			return err
		}
		return os.Mkdir(filepath.Join(root, "safe"), 0700)
	})
	if core.CodeOf(err) != core.Conflict {
		t.Fatal("committed into rebound parent", err)
	}
	if _, err = os.Stat(filepath.Join(root, "moved", "note")); !os.IsNotExist(err) {
		t.Fatal("wrote into stale directory")
	}
}

func TestConcurrentFileAPIsCommitOnlyOneExpectedVersion(t *testing.T) {
	root := t.TempDir()
	ctx := t.Context()
	version, err := Replace(ctx, root, "shared", "initial", false, "", func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	results := make(chan error, 2)
	for _, text := range []string{"memory API", "file tool"} {
		go func() {
			_, e := Replace(ctx, root, "shared", text, true, version, func() error { ready <- struct{}{}; <-release; return nil })
			results <- e
		}()
	}
	<-ready
	<-ready
	close(release)
	first, second := <-results, <-results
	if (first == nil) == (second == nil) {
		t.Fatal("same file revision committed twice", first, second)
	}
	if first != nil && core.CodeOf(first) != core.Conflict || second != nil && core.CodeOf(second) != core.Conflict {
		t.Fatal(first, second)
	}
}

func TestMoveAndRemoveDirectoryDoNotReplaceTargets(t *testing.T) {
	root := t.TempDir()
	ctx := t.Context()
	allow := func() error { return nil }
	if err := MkdirAll(root, "中文目录"); err != nil {
		t.Fatal(err)
	}
	version, err := Replace(ctx, root, "中文目录/主题.md", "first", false, "", allow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Replace(ctx, root, "中文目录/已有文件.md", "keep", false, "", allow); err != nil {
		t.Fatal(err)
	}
	if _, err = Move(ctx, root, "中文目录/主题.md", "中文目录/已有文件.md", version, false, allow); core.CodeOf(err) != core.Conflict {
		t.Fatal(err)
	}
	if _, err = Move(ctx, root, "中文目录/主题.md", "中文目录/新主题.md", version, false, allow); err != nil {
		t.Fatal(err)
	}
	d, err := OpenDirectory(root, "中文目录")
	if err != nil {
		t.Fatal(err)
	}
	v, err := Version(d)
	d.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err = RemoveDirectory(ctx, root, "中文目录", v, allow); core.CodeOf(err) != core.Conflict {
		t.Fatal("removed nonempty directory", err)
	}
	if _, err = Move(ctx, root, "中文目录", "新目录", v, true, allow); err != nil {
		t.Fatal(err)
	}
	if err = MkdirAll(root, "空目录"); err != nil {
		t.Fatal(err)
	}
	d, err = OpenDirectory(root, "空目录")
	if err != nil {
		t.Fatal(err)
	}
	v, err = Version(d)
	d.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err = RemoveDirectory(ctx, root, "空目录", v, allow); err != nil {
		t.Fatal(err)
	}
}
