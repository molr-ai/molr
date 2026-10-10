package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/core"
)

func diskFixture(t *testing.T) (*Files, string, *service) {
	t.Helper()
	root := t.TempDir()
	files := NewFiles(FileOptions{JournalRoot: filepath.Join(root, "journal"), Roots: func(ctx context.Context, owner core.ID, query RootQuery) ([]Root, error) {
		return []Root{{Scope: Global, Directory: filepath.Join(root, string(owner), "global")}, {Scope: Assistant, AssistantID: "assistant", Directory: filepath.Join(root, string(owner), "work")}, {Scope: Assistant, AssistantID: "another", Directory: filepath.Join(root, string(owner), "another")}}, nil
	}})
	s := NewService(fixtureDB(t), Options{Files: files, Authorizer: fixtureAuthorizer{}, CheckAssistant: func(context.Context, core.Principal, core.ID) error { return nil }}).(*service)
	return files, root, s
}
func TestDiskFilesExternalEditsSummaryAndRestart(t *testing.T) {
	files, root, s := diskFixture(t)
	ctx := t.Context()
	work := filepath.Join(root, "owner", "work")
	if err := os.MkdirAll(filepath.Join(work, ".agents/memory/notes"), 0700); err != nil {
		t.Fatal(err)
	}
	raw := "# Summary\nAlways write short answers.\n"
	if err := os.WriteFile(filepath.Join(work, "MEMORY.md"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(work, ".agents/memory/notes/preference.md")
	if err := os.WriteFile(name, []byte("Prefer tea.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	page, err := s.List(ctx, agent(), Query{Scope: Assistant})
	if err != nil || len(page.Items) != 2 {
		t.Fatal(page, err)
	}
	var m Memory
	for _, v := range page.Items {
		if !v.Summary {
			m = v
		}
	}
	summary, err := s.Get(ctx, agent(), Ref{Scope: Assistant, AssistantID: "assistant", Path: "MEMORY.md"})
	if err != nil || !summary.Summary {
		t.Fatal(summary, err)
	}

	if m.Version != hash([]byte("Prefer tea.\n")) {
		t.Fatal("revision is not the complete file hash")
	}
	if err = os.WriteFile(name, []byte("Prefer coffee.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = s.Update(ctx, owner(), UpdateRequest{Ref: m.Ref, Text: "stale overwrite", Mutation: mutation("stale", m.Version)})
	wantCode(t, err, core.Conflict)
	restarted := NewFiles(files.options)
	current, err := restarted.Get(ctx, "owner", m.Ref)
	if err != nil || current.Text != "Prefer coffee.\n" || current.Version == m.Version {
		t.Fatal(current, err)
	}
	if err = os.Remove(name); err != nil {
		t.Fatal(err)
	}
	_, err = restarted.Get(ctx, "owner", m.Ref)
	wantCode(t, err, core.NotFound)
	if fixtureCount(t, s.store, "business.memories") != 0 || fixtureCount(t, s.store, "business.memories.receipts") != 0 {
		t.Fatal("database memory mirror created")
	}
}
func TestDiskMutationReplayDoesNotUndoExternalChanges(t *testing.T) {
	_, root, s := diskFixture(t)
	ctx := t.Context()
	req := CreateRequest{Text: "Original.", Mutation: mutation("create", ""), Ref: Ref{Scope: Assistant, AssistantID: "assistant", Path: ".agents/memory/create.md"}}
	m, err := s.Create(ctx, owner(), req)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(root, "owner", "work", strings.TrimPrefix(m.Path, "/work/"))
	if err = os.WriteFile(filename, []byte("Human edit.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = s.Create(ctx, owner(), req)
	wantCode(t, err, core.Conflict)
	if err = os.Remove(filename); err != nil {
		t.Fatal(err)
	}
	_, err = s.Create(ctx, owner(), req)
	wantCode(t, err, core.Conflict)
	if _, err = os.Stat(filename); !os.IsNotExist(err) {
		t.Fatal("replay recreated deleted file")
	}
}
func TestDiskForgetRecordsAndCloneExport(t *testing.T) {
	files, root, s := diskFixture(t)
	ctx := t.Context()
	m, err := s.Create(ctx, owner(), CreateRequest{Text: "SECRET_PRIVATE_TEXT", Sources: []core.Object{{Kind: "message", ID: "source"}}, Mutation: mutation("create", ""), Ref: Ref{Scope: Assistant, AssistantID: "assistant", Path: ".agents/memory/create.md"}})
	if err != nil {
		t.Fatal(err)
	}
	req := DeleteRequest{Forget: true, Ref: m.Ref, Mutation: mutation("forget", m.Version)}
	if err = s.Delete(ctx, owner(), req); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(ctx, owner(), req); err != nil {
		t.Fatal("replayed forget", err)
	}
	blocked, err := files.Suppressed(ctx, m)
	if err != nil || !blocked {
		t.Fatal(blocked, err)
	}
	copied := map[string]string{}
	if err = files.Export(ctx, "owner", func(name string, b []byte) error { copied[name] = string(b); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(copied) != 1 {
		t.Fatal(copied)
	}
	for name, text := range copied {
		if !strings.Contains(name, ".forgotten/") || strings.Contains(text, m.Text) {
			t.Fatal("forget record contains text", name, text)
		}
	}
	fresh, err := s.Create(ctx, owner(), CreateRequest{Text: "Clone this", Mutation: mutation("fresh", ""), Ref: Ref{Scope: Assistant, AssistantID: "assistant", Path: ".agents/memory/fresh.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = files.Clone(ctx, owner(), "assistant", "another"); err != nil {
		t.Fatal(err)
	}
	page, err := files.List(ctx, "owner", Query{AssistantID: "another"})
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	dest := filepath.Join(root, "owner", "another", strings.TrimPrefix(fresh.Path, "/work/"))
	if err = os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	if err = files.Clone(ctx, owner(), "assistant", "another"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("clone replay recreated deleted file")
	}
	m.AssistantID = "another"
	blocked, err = files.Suppressed(ctx, m)
	if err != nil || !blocked {
		t.Fatal("clone lost forgetting records", err)
	}
}
func TestDiskRefusesLinksAndAtomicCommitCancellation(t *testing.T) {
	files, root, _ := diskFixture(t)
	ctx := t.Context()
	m := Memory{OwnerID: "owner", Text: "Before", Ref: Ref{AssistantID: "assistant", Scope: Assistant, Path: ".agents/memory/item.md"}}
	m, err := files.Put(ctx, m, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	original := m
	m.Text = "After"
	_, err = files.Put(ctx, m, m.Version, func() error { return core.NewError(core.Canceled, "stopped") })
	wantCode(t, err, core.Canceled)
	current, err := files.Get(ctx, "owner", m.Ref)
	if err != nil || current.Version != original.Version {
		t.Fatal("failed commit changed file", current, err)
	}
	outside := filepath.Join(root, "outside.md")
	if err = os.WriteFile(outside, []byte("Outside"), 0600); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(root, "owner", "work", ".agents/memory/item.md")
	if err = os.Remove(filename); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filename); err != nil {
		t.Fatal(err)
	}
	_, err = files.Put(ctx, m, m.Version, nil)
	wantCode(t, err, core.Forbidden)
	if err = os.Remove(filename); err != nil {
		t.Fatal(err)
	}
	if err = os.Link(outside, filename); err != nil {
		t.Fatal(err)
	}
	_, err = files.Get(ctx, "owner", m.Ref)
	wantCode(t, err, core.Forbidden)
	b, _ := os.ReadFile(outside)
	if string(b) != "Outside" {
		t.Fatal("outside file changed")
	}
}

func TestPendingCreateReceiptDoesNotResurrectMissingFile(t *testing.T) {
	files, root, s := diskFixture(t)
	ctx := t.Context()
	req := CreateRequest{Text: "Original", Mutation: mutation("uncertain", ""), Ref: Ref{Scope: Assistant, AssistantID: "assistant", Path: ".agents/memory/uncertain.md"}}
	m, err := s.Create(ctx, owner(), req)
	if err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(files.options.JournalRoot, hash([]byte("owner")))
	entries, err := os.ReadDir(journal)
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	receiptPath := filepath.Join(journal, entries[0].Name())
	data, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var r receipt
	if err = json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	r.Done = false
	data, _ = json.Marshal(r)
	if err = os.WriteFile(receiptPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(root, "owner", "work", strings.TrimPrefix(m.Path, "/work/"))
	if err = os.Remove(filename); err != nil {
		t.Fatal(err)
	}
	_, err = s.Create(ctx, owner(), req)
	wantCode(t, err, core.Conflict)
	if _, err = os.Stat(filename); !os.IsNotExist(err) {
		t.Fatal("pending create restored a missing file")
	}
}

func TestSemanticPathsDirectoryMoveAndExplicitForget(t *testing.T) {
	_, _, s := diskFixture(t)
	ctx := t.Context()
	dir := Ref{Scope: Assistant, AssistantID: "assistant", Path: ".agents/memory/用户偏好"}
	if dir.LogicalPath() != "/work/.agents/memory/用户偏好" {
		t.Fatal(dir.LogicalPath())
	}
	wantCode(t, dir.Validate(false), core.InvalidArgument)
	if err := dir.Validate(true); err != nil {
		t.Fatal(err)
	}
	if err := s.Mkdir(ctx, owner(), DirectoryRequest{Ref: dir, Mutation: mutation("mkdir", "")}); err != nil {
		t.Fatal(err)
	}
	listing, err := s.Browse(ctx, owner(), dir)
	if err != nil || len(listing.Entries) != 0 || listing.Version == "" {
		t.Fatal(listing, err)
	}
	ref := dir
	ref.Path += "/沟通偏好.md"
	req := CreateRequest{Ref: ref, Text: "先给结论", Mutation: mutation("semantic", "")}
	m, err := s.Create(ctx, owner(), req)
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(m)
	var fields map[string]any
	json.Unmarshal(wire, &fields)
	if fields["Path"] != ref.Path || fields["ID"] != nil || fields["Category"] != nil {
		t.Fatal("item identity survived", string(wire))
	}
	req.Mutation = mutation("same-path", "")
	_, err = s.Create(ctx, owner(), req)
	wantCode(t, err, core.Conflict)
	listing, err = s.Browse(ctx, owner(), dir)
	if err != nil || len(listing.Entries) != 1 || listing.Entries[0].Path != ref.Path {
		t.Fatal(listing, err)
	}
	wantCode(t, s.RemoveDirectory(ctx, owner(), DirectoryRequest{Ref: dir, Mutation: mutation("nonempty", listing.Version)}), core.Conflict)
	to := dir.Path + "/工作沟通.md"
	move := MoveRequest{Ref: ref, ToPath: to, Mutation: mutation("move", m.Version)}
	if err = s.Move(ctx, owner(), move); err != nil {
		t.Fatal(err)
	}
	if err = s.Move(ctx, owner(), move); err != nil {
		t.Fatal("move replay", err)
	}
	_, err = s.Get(ctx, owner(), ref)
	wantCode(t, err, core.NotFound)
	ref.Path = to
	m, err = s.Get(ctx, owner(), ref)
	if err != nil || m.Text != "先给结论" {
		t.Fatal(m, err)
	}
	if err = s.Delete(ctx, owner(), DeleteRequest{Ref: ref, Mutation: mutation("raw-delete", m.Version)}); err != nil {
		t.Fatal(err)
	}
	blocked, err := s.repo.Suppressed(ctx, m)
	if err != nil || blocked {
		t.Fatal("ordinary deletion created forgetting rules", err)
	}
	listing, err = s.Browse(ctx, owner(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Move(ctx, owner(), MoveRequest{Ref: dir, Directory: true, ToPath: ".agents/memory/偏好归档", Mutation: mutation("move-folder", listing.Version)}); err != nil {
		t.Fatal(err)
	}
	dir.Path = ".agents/memory/偏好归档"
	listing, err = s.Browse(ctx, owner(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RemoveDirectory(ctx, owner(), DirectoryRequest{Ref: dir, Mutation: mutation("empty-folder", listing.Version)}); err != nil {
		t.Fatal(err)
	}
}

func TestReviewFilesUseSemanticPathsAndForgetSourceReferences(t *testing.T) {
	files, root, s := diskFixture(t)
	ctx := t.Context()
	ref := Ref{Scope: Assistant, AssistantID: "assistant", Path: ".agents/memory/用户偏好/沟通偏好.md"}
	sources := []core.Object{{Kind: "message", ID: "source"}}
	m, err := s.Create(ctx, owner(), CreateRequest{Ref: ref, Text: "先给结论", Sources: sources, Mutation: mutation("fact", "")})
	if err != nil {
		t.Fatal(err)
	}
	review := ".agents/reviews/2026-10-06/第1批回顾.md"
	version, err := files.WriteReview(ctx, "owner", "assistant", "run", review, "用户希望先给结论", sources, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err = files.WriteReview(ctx, "owner", "assistant", "run", review, "用户希望先给结论", sources, func() error { return nil }); err != nil {
		t.Fatal("review replay", err)
	}
	retained, err := files.RemoveReview(ctx, "owner", "assistant", review, "stale", func() error { return nil })
	if err != nil || !retained {
		t.Fatal("stale review removed", err)
	}
	if err = s.Delete(ctx, owner(), DeleteRequest{Ref: ref, Forget: true, Mutation: mutation("forget-review", m.Version)}); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "owner", "work", review)); !os.IsNotExist(err) {
		t.Fatal("forgotten review retained", err)
	}
	if _, err = files.WriteReview(ctx, "owner", "assistant", "run", review, "用户希望先给结论", sources, func() error { return nil }); core.CodeOf(err) != core.Conflict {
		t.Fatal("review resurrected", version, err)
	}
}
