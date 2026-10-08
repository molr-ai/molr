package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDreamHistoryRestoresExactBytesAndProtectsLaterEdits(t *testing.T) {
	files, root, _ := diskFixture(t)
	ref := Ref{Scope: Assistant, AssistantID: "assistant", Path: ".agents/memory/约定.md"}
	dir := filepath.Join(root, "owner", "work", ".agents", "memory")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	before := "# 原有约定\n\n保留原始格式。  \n"
	if err := os.WriteFile(filepath.Join(dir, "约定.md"), []byte(before), 0600); err != nil {
		t.Fatal(err)
	}
	old, err := files.Get(t.Context(), "owner", ref)
	if err != nil {
		t.Fatal(err)
	}
	next := old
	next.Text = "自动修订"
	next.DreamID = "dream"
	next, err = files.Preview(t.Context(), next)
	if err != nil {
		t.Fatal(err)
	}
	if err = files.BackupDream(t.Context(), "dream", next, old.Version, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = files.Put(t.Context(), next, old.Version, nil); err != nil {
		t.Fatal(err)
	}
	restored, err := files.RestoreDream(t.Context(), "owner", "dream", ref, next.Version, nil)
	if err != nil || !restored {
		t.Fatal(restored, err)
	}
	actual, _ := os.ReadFile(filepath.Join(dir, "约定.md"))
	if string(actual) != before {
		t.Fatal("restore changed original bytes")
	}
	restored, err = files.RestoreDream(t.Context(), "owner", "dream", ref, next.Version, nil)
	if err != nil || !restored {
		t.Fatal("interrupted undo not idempotent", err)
	}
	if err = os.WriteFile(filepath.Join(dir, "约定.md"), []byte("后来的用户编辑"), 0600); err != nil {
		t.Fatal(err)
	}
	restored, err = files.RestoreDream(t.Context(), "owner", "dream", ref, next.Version, nil)
	if err != nil || restored {
		t.Fatal("undo overwrote later edit", err)
	}
}
