package memory

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"molr.ai/runtime/internal/core"
)

func pageFile(t *testing.T, root, name, body string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
func pageFixture(t *testing.T) (*Files, string) {
	t.Helper()
	root := t.TempDir()
	files := NewFiles(FileOptions{Roots: func(context.Context, core.ID) ([]Root, error) {
		return []Root{{Scope: Assistant, AssistantID: "assistant", Directory: root}}, nil
	}})
	return files, root
}

func TestMemoryPagesMatchFullScanOrderAndFilters(t *testing.T) {
	files, root, _ := diskFixture(t)
	for _, dir := range []string{"global", "work", "another"} {
		for i, name := range []string{"MEMORY.md", ".agents/memory/a.md", ".agents/memory/a/nested.md", ".agents/memory/z.md"} {
			state := "active"
			if i%2 == 0 {
				state = "pending"
			}
			pageFile(t, filepath.Join(root, "owner", dir), name, "---\nstate: "+state+"\n---\nCafé 東京 "+dir+name)
		}
	}
	for _, q := range []Query{{}, {Scope: Global}, {AssistantID: "assistant"}, {State: Pending}, {Text: "CAFÉ"}, {Scope: Assistant, State: Active, Text: "NESTED"}} {
		expected, err := files.all(t.Context(), "owner", q)
		if err != nil {
			t.Fatal(err)
		}
		for _, limit := range []int{1, 2, 5, 200} {
			q.Page = core.PageRequest{Limit: limit}
			got := []Memory{}
			pages := 0
			for {
				page, err := files.List(t.Context(), "owner", q)
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, page.Items...)
				pages++
				if pages > 20 {
					t.Fatal("cursor did not advance")
				}
				if page.NextCursor == "" {
					break
				}
				q.Page.Cursor = page.NextCursor
			}
			if !reflect.DeepEqual(got, expected) {
				t.Fatalf("page/filter mismatch %+v: got %d expected %d", q, len(got), len(expected))
			}
		}
	}
}

func TestMemoryPageSkipsPriorBodiesAndDefersUnreadCorruptTail(t *testing.T) {
	files, root := pageFixture(t)
	for i := 0; i < 60; i++ {
		pageFile(t, root, fmt.Sprintf(".agents/memory/%03d.md", i), "valid memory")
	}
	pageFile(t, root, ".agents/memory/051.md", "---\ninvalid beyond lookahead")
	pageFile(t, root, ".agents/memory/058.md", "---\ninvalid header")
	q := Query{Page: core.PageRequest{Limit: 50}}
	first, err := files.List(t.Context(), "owner", q)
	if err != nil || len(first.Items) != 50 || first.NextCursor == "" {
		t.Fatal("read beyond page/lookahead", len(first.Items), err)
	}
	pageFile(t, root, ".agents/memory/051.md", "valid memory")
	for i := 0; i < 50; i++ {
		pageFile(t, root, fmt.Sprintf(".agents/memory/%03d.md", i), "---\ncorrupted prior page")
	}
	q.Page = core.PageRequest{Limit: 5, Cursor: first.NextCursor}
	second, err := files.List(t.Context(), "owner", q)
	if err != nil || len(second.Items) != 5 || second.Items[0].Path != ".agents/memory/050.md" {
		t.Fatal("re-read prior bodies", second, err)
	}
	q.Page.Cursor = second.NextCursor
	if _, err := files.List(t.Context(), "owner", q); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("corrupt selected body was hidden", err)
	}
}

func TestMemoryCursorSurvivesDeletedBoundaryAndReadsExternalEdits(t *testing.T) {
	files, root := pageFixture(t)
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		pageFile(t, root, ".agents/memory/"+name+".md", name)
	}
	q := Query{Page: core.PageRequest{Limit: 2}}
	first, err := files.List(t.Context(), "owner", q)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		if err := os.Remove(filepath.Join(root, ".agents/memory/"+name+".md")); err != nil {
			t.Fatal(err)
		}
	}
	pageFile(t, root, ".agents/memory/aa.md", "inserted before cursor")
	pageFile(t, root, ".agents/memory/c.md", "edited outside Runtime")
	q.Page.Cursor = first.NextCursor
	next, err := files.List(t.Context(), "owner", q)
	if err != nil || len(next.Items) != 2 || next.Items[0].Path != ".agents/memory/c.md" || next.Items[0].Text != "edited outside Runtime" || next.Items[0].Version != hash([]byte("edited outside Runtime")) {
		t.Fatal(next, err)
	}
}

func TestMemoryCursorBindingValidationAndLongNames(t *testing.T) {
	files, root := pageFixture(t)
	name := ".agents/memory/" + strings.Repeat("a"+strings.Repeat("<&\"", 60)+"/", 3) + strings.Repeat("文", 60) + ".md"
	pageFile(t, root, name, "long valid path")
	pageFile(t, root, ".agents/memory/z.md", "last")
	q := Query{Page: core.PageRequest{Limit: 1}}
	page, err := files.List(t.Context(), "owner", q)
	if err != nil || len(page.Items) != 1 || page.Items[0].Path != name || len(page.NextCursor) > 2048 {
		t.Fatal(page, err)
	}
	q.Page.Cursor = page.NextCursor
	next, err := files.List(t.Context(), "owner", q)
	if err != nil || len(next.Items) != 1 || next.Items[0].Path != ".agents/memory/z.md" {
		t.Fatal(next, err)
	}
	calls := 0
	roots := files.options.Roots
	files.options.Roots = func(ctx context.Context, id core.ID) ([]Root, error) { calls++; return roots(ctx, id) }
	for _, change := range []func(*Query){func(q *Query) { q.Scope = Global }, func(q *Query) { q.AssistantID = "another" }, func(q *Query) { q.State = Pending }, func(q *Query) { q.Text = "new" }} {
		changed := q
		change(&changed)
		if _, err := files.List(t.Context(), "owner", changed); core.CodeOf(err) != core.InvalidArgument {
			t.Fatal("cross-query cursor", err)
		}
	}
	if _, err := files.List(t.Context(), "other", q); core.CodeOf(err) != core.InvalidArgument {
		t.Fatal("cross-owner cursor", err)
	}
	for _, cursor := range []string{"invalid!", base64.RawURLEncoding.EncodeToString([]byte("invalid\x00assistant\x00assistant\x00../escape.md"))} {
		bad := q
		bad.Page.Cursor = cursor
		if _, err := files.List(t.Context(), "owner", bad); core.CodeOf(err) != core.InvalidArgument {
			t.Fatal(err)
		}
	}
	if calls != 0 {
		t.Fatal("invalid cursor opened roots", calls)
	}
}

func TestMemoryPaginationKeepsScanAndLinkBounds(t *testing.T) {
	t.Run("content", func(t *testing.T) {
		files, root := pageFixture(t)
		body := strings.Repeat("x", 65536)
		for i := 0; i < 140; i++ {
			pageFile(t, root, fmt.Sprintf(".agents/memory/%03d.md", i), body)
		}
		if _, err := files.List(t.Context(), "owner", Query{State: Pending, Page: core.PageRequest{Limit: 1}}); core.CodeOf(err) != core.FailedPrecondition {
			t.Fatal("content scan bound lost", err)
		}
		page, err := files.List(t.Context(), "owner", Query{Page: core.PageRequest{Limit: 1}})
		if err != nil || len(page.Items) != 1 {
			t.Fatal("small page scanned all bodies", err)
		}
	})
	t.Run("entries", func(t *testing.T) {
		files, root := pageFixture(t)
		for i := 0; i < 4097; i++ {
			pageFile(t, root, fmt.Sprintf(".agents/memory/%04d.txt", i), "entry")
		}
		if _, err := files.List(t.Context(), "owner", Query{Page: core.PageRequest{Limit: 1}}); core.CodeOf(err) != core.FailedPrecondition {
			t.Fatal(err)
		}
	})
	t.Run("depth", func(t *testing.T) {
		files, root := pageFixture(t)
		pageFile(t, root, ".agents/memory/"+strings.Repeat("nested/", 17)+"item.md", "deep")
		if _, err := files.List(t.Context(), "owner", Query{}); core.CodeOf(err) != core.FailedPrecondition {
			t.Fatal(err)
		}
	})
	t.Run("hard link", func(t *testing.T) {
		files, root := pageFixture(t)
		pageFile(t, root, ".agents/memory/a.md", "valid")
		outside := filepath.Join(t.TempDir(), "outside.md")
		if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(outside, filepath.Join(root, ".agents/memory/b.md")); err != nil {
			t.Fatal(err)
		}
		if _, err := files.List(t.Context(), "owner", Query{}); core.CodeOf(err) != core.Forbidden {
			t.Fatal(err)
		}
	})
	t.Run("canceled", func(t *testing.T) {
		files, _ := pageFixture(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := files.List(ctx, "owner", Query{}); err == nil {
			t.Fatal("canceled scan succeeded")
		}
	})
}
