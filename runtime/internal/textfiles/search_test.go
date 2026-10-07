package textfiles

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/core"
)

func TestSearchContentPathsLimitsAndHiddenRoot(t *testing.T) {
	root := t.TempDir()
	dir := ".agents/memory/用户偏好"
	if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, dir, "沟通偏好.md"), []byte("前文\nPrefer 中文\n后文\n"), 0600); err != nil {
		t.Fatal(err)
	}
	check := func(string) error { return nil }
	r, err := Search(t.Context(), root, ".agents/memory", SearchOptions{Pattern: "prefer|中文", Regex: true, Glob: []string{"**/*.md"}, ContextLines: 1}, check)
	if err != nil || !r.Complete || len(r.Matches) != 1 || r.Matches[0].Line != 2 || len(r.Matches[0].Before) != 1 || len(r.Matches[0].After) != 1 {
		t.Fatal(r, err)
	}
	r, err = Search(t.Context(), root, ".", SearchOptions{Pattern: "中文"}, check)
	if err != nil || len(r.Matches) != 0 {
		t.Fatal("hidden descendants searched", r, err)
	}
	r, err = Search(t.Context(), root, dir+"/沟通偏好.md", SearchOptions{Pattern: "中文", MaxResults: 1}, check)
	if err != nil || len(r.Matches) != 1 || !r.Truncated {
		t.Fatal("single file or limit", r, err)
	}
	r, err = Search(t.Context(), root, ".agents/memory", SearchOptions{Mode: "files", Glob: []string{"**/沟通*.md"}}, check)
	if err != nil || len(r.Matches) != 1 || r.Matches[0].Text != "" {
		t.Fatal(r, err)
	}
}
func TestSearchRejectsInvalidRegexAndRechecksAuthorization(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "secret.md"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Search(t.Context(), root, ".", SearchOptions{Pattern: "[", Regex: true}, func(string) error { return nil })
	if core.CodeOf(err) != core.InvalidArgument {
		t.Fatal(err)
	}
	_, err = Search(t.Context(), root, ".", SearchOptions{Pattern: "secret"}, func(name string) error {
		if name == "secret.md" {
			return core.NewError(core.Forbidden, "revoked")
		}
		return nil
	})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("child permission bypass", err)
	}
}

func TestSearchOutputBudgetIncludesJSONEscaping(t *testing.T) {
	root := t.TempDir()
	text := strings.Repeat(strings.Repeat("<", 2000)+"\n", 10)
	if err := os.WriteFile(filepath.Join(root, "escaped.md"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Search(t.Context(), root, ".", SearchOptions{Pattern: "<", ContextLines: 1}, func(string) error { return nil })
	encoded, _ := json.Marshal(result)
	if err != nil || len(encoded) > 32<<10 || result.Complete || !result.Truncated || result.StopReason != "output_limit" {
		t.Fatal(len(encoded), result, err)
	}
}
