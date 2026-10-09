package memory

import (
	"molr.ai/runtime/internal/core"
	"os"
	"path/filepath"
	"testing"
)

func TestDreamHistoryObservesCommonToolEffectsAndProtectsLaterEdits(t *testing.T) {
	f, root, _ := diskFixture(t)
	ctx := t.Context()
	check := func() error { return nil }
	ref := Ref{Scope: Assistant, AssistantID: "assistant", Path: ".agents/memory/note.md"}
	dest := filepath.Join(root, "owner", "work", ref.Path)
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		t.Fatal(err)
	}
	changes := []FileChange{{Ref: ref, After: []byte("created by file tool"), AfterExists: true}}
	if err := f.PrepareFileOperation(ctx, "owner", "create", "digest", changes, check); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("journal preparation performed a file write")
	}
	if err := f.CompleteFileOperation(ctx, "owner", "create", check); err == nil {
		t.Fatal("recorded an effect that never happened")
	}
	if err := os.WriteFile(dest, changes[0].After, 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.CompleteFileOperation(ctx, "owner", "create", check); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("user correction"), 0600); err != nil {
		t.Fatal(err)
	}
	kept, err := f.UndoFileOperation(ctx, "owner", "create", check)
	if err != nil || len(kept) != 1 {
		t.Fatal(kept, err)
	}
	if err := os.WriteFile(dest, changes[0].After, 0600); err != nil {
		t.Fatal(err)
	}
	kept, err = f.UndoFileOperation(ctx, "owner", "create", check)
	if err != nil || len(kept) != 0 {
		t.Fatal(kept, err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("new file not undone")
	}
}
func TestDreamHistoryRestoresDeletedFileWithoutFollowingLinks(t *testing.T) {
	f, root, _ := diskFixture(t)
	ctx := t.Context()
	check := func() error { return nil }
	ref := Ref{Scope: Assistant, AssistantID: "assistant", Path: ".agents/memory/note.md"}
	dest := filepath.Join(root, "owner", "work", ref.Path)
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		t.Fatal(err)
	}
	raw := []byte("retain this prior content")
	if err := os.WriteFile(dest, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.PrepareFileOperation(ctx, "owner", "delete", "digest", []FileChange{{Ref: ref, Before: raw, BeforeExists: true}}, check); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	if err := f.CompleteFileOperation(ctx, "owner", "delete", check); err != nil {
		t.Fatal(err)
	}
	if kept, err := f.UndoFileOperation(ctx, "owner", "delete", check); err != nil || len(kept) != 0 {
		t.Fatal(kept, err)
	}
	data, err := os.ReadFile(dest)
	if err != nil || string(data) != string(raw) {
		t.Fatal(string(data), err)
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err = os.WriteFile(outside, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, dest); err != nil {
		t.Fatal(err)
	}
	if _, err = f.ReadFile(ctx, "owner", ref); core.CodeOf(err) != core.Forbidden {
		t.Fatal("followed link", err)
	}
}
