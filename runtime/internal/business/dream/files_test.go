package dream

import (
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/textfiles"
	"os"
	"path"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, f *fixture, r Run, operation, name, text, version string) memory.FileSnapshot {
	t.Helper()
	ref, err := FileRef(f.ap, name, false)
	if err != nil {
		t.Fatal(err)
	}
	done, err := f.s.BeforeFileChange(t.Context(), f.ap, ev(r), core.ID(operation), "file_write", ref, nil, version, []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(f.root, "owner", "work")
	if err = os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err = textfiles.MkdirAll(root, path.Dir(ref.Path)); err != nil {
		t.Fatal(err)
	}
	if _, err = textfiles.Replace(t.Context(), root, ref.Path, text, version != "", version, func() error { return f.s.CheckFileAccess(t.Context(), f.ap, ev(r), true) }); err != nil {
		t.Fatal(err)
	}
	if err = done(); err != nil {
		t.Fatal(err)
	}
	doc, err := f.s.options.Files.ReadFile(t.Context(), "owner", ref)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
func TestDreamSummaryCannotPerformFileWrites(t *testing.T) {
	f := setup(t)
	f.message(t, 1)
	r := f.request(t, "summary")
	if err := f.s.Commit(t.Context(), f.ap, ev(r), `{"Memories":[{"Path":"MEMORY.md","Text":"must not write"}]}`); err != nil {
		t.Fatal(err)
	}
	page, _ := f.memories.List(t.Context(), f.p, memory.Query{})
	if len(page.Items) != 0 {
		t.Fatal("summary executed as old write plan", page)
	}
}

func TestForgetToolGeneratedKnowledgeRemovesCoreAndSources(t *testing.T) {
	f := setup(t)
	f.message(t, 1)
	r := f.request(t, "forget")
	doc := writeFile(t, f, r, "topic", ".agents/memory/约定.md", "项目汇报先写结论", "")
	writeFile(t, f, r, "core", "MEMORY.md", "汇报先写结论", "")
	if err := f.s.Commit(t.Context(), f.ap, ev(r), "整理汇报约定。"); err != nil {
		t.Fatal(err)
	}
	ref, _ := FileRef(f.ap, doc.Path, false)
	if err := f.memories.Delete(t.Context(), f.p, memory.DeleteRequest{Ref: ref, Forget: true, Mutation: memory.Mutation{IdempotencyKey: "forget", ExpectedVersion: doc.Version}}); err != nil {
		t.Fatal(err)
	}
	page, err := f.memories.List(t.Context(), f.p, memory.Query{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("derived core retained", page, err)
	}
	if _, err = f.s.Undo(t.Context(), f.p, UndoRequest{ID: r.ID, Mutation: core.Mutation{IdempotencyKey: "undo"}}); err != nil {
		t.Fatal(err)
	}
	page, _ = f.memories.List(t.Context(), f.p, memory.Query{})
	if len(page.Items) != 0 {
		t.Fatal("undo revived forgotten information", page)
	}
}
