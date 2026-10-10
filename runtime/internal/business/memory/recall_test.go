package memory

import (
	"context"
	"molr.ai/runtime/internal/core"
	"path/filepath"
	"testing"
)

func TestHybridRecallAndSuppression(t *testing.T) {
	db := fixtureDB(t)
	root := t.TempDir()
	repo := NewFiles(FileOptions{JournalRoot: filepath.Join(root, "journal"), Roots: func(ctx context.Context, owner core.ID, query RootQuery) ([]Root, error) {
		return []Root{{Scope: Assistant, AssistantID: "assistant", Directory: filepath.Join(root, string(owner), "assistant")}, {Scope: Assistant, AssistantID: "other", Directory: filepath.Join(root, string(owner), "other")}}, nil
	}})
	s := NewService(db, Options{Files: repo, Authorizer: fixtureAuthorizer{}})
	p := core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "execution"}
	m, err := repo.Put(t.Context(), Memory{Ref: Ref{Scope: Assistant, AssistantID: "assistant", Path: ".agents/memory/项目汇报.md"}, OwnerID: "owner", Text: "项目周报先写结论"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.List(t.Context(), p, Query{Scope: Assistant})
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	if err = repo.Delete(t.Context(), m, m.Version, true, nil); err != nil {
		t.Fatal(err)
	}

	blocked, err := repo.Suppressed(t.Context(), m)
	if err != nil || !blocked {
		t.Fatal(blocked, err)
	}
	m.AssistantID = "other"
	blocked, err = repo.Suppressed(t.Context(), m)
	if err != nil || blocked {
		t.Fatal("forget scope leaked", err)
	}
}
