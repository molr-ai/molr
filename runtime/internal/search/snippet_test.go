package search

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func TestSearchSnippetPreservesRuneBoundaries(t *testing.T) {
	db := fixtureDB(t)
	for _, tc := range []struct{ name, input, want string }{
		{"empty", "", ""},
		{"short", "中文 🙂", "中文 🙂"},
		{"exact", strings.Repeat("文", 240), strings.Repeat("文", 240)},
		{"chinese", strings.Repeat("测试", 1000), strings.Repeat("测试", 120)},
		{"emoji_boundary", strings.Repeat("a", 239) + "🙂界", strings.Repeat("a", 239) + "🙂"},
		{"combining_boundary", strings.Repeat("a", 239) + "\u0301z", strings.Repeat("a", 239) + "\u0301"},
		{"invalid_utf8", "ab\xff\xfe" + strings.Repeat("文", 240), "ab��" + strings.Repeat("文", 236)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := Document{OwnerID: "owner", Source: Messages, Object: core.Object{Kind: "message", ID: "message"}, Title: "match", Text: tc.input}
			s := NewService(db, Options{Authorizer: fixtureAuthorizer{}, Documents: func(context.Context, core.ID) ([]Document, error) { return []Document{doc}, nil }})
			result, err := s.Search(t.Context(), owner(), Query{Text: "match", Sources: []Source{Messages}})
			if err != nil || len(result.Hits.Items) != 1 {
				t.Fatalf("hits=%d error=%v", len(result.Hits.Items), err)
			}
			hit := result.Hits.Items[0]
			if hit.Snippet != tc.want || !utf8.ValidString(hit.Snippet) || hit.Score != 1 || result.Counts[Messages] != 1 {
				t.Fatalf("unexpected snippet or search metadata: %+v", hit)
			}
		})
	}
}

func TestSearchSnippetKeepsTailMatchesScoresAndPagination(t *testing.T) {
	db := fixtureDB(t)
	prefix := strings.Repeat("文", 240)
	docs := []Document{
		{OwnerID: "owner", Source: Messages, Object: core.Object{Kind: "message", ID: "once"}, Text: prefix + " needle"},
		{OwnerID: "owner", Source: Messages, Object: core.Object{Kind: "message", ID: "twice"}, Text: prefix + " needle needle"},
		{OwnerID: "other", Source: Messages, Object: core.Object{Kind: "message", ID: "foreign"}, Text: prefix + " needle needle needle"},
		{OwnerID: "owner", Source: Messages, Object: core.Object{Kind: "message", ID: "archived"}, Text: prefix + " needle", Archived: true},
	}
	s := NewService(db, Options{Authorizer: fixtureAuthorizer{}, Documents: func(context.Context, core.ID) ([]Document, error) { return docs, nil }})
	q := Query{Text: "needle", Sources: []Source{Messages}, Page: core.PageRequest{Limit: 1}}
	first, err := s.Search(t.Context(), owner(), q)
	if err != nil || len(first.Hits.Items) != 1 || first.Hits.Items[0].Object.ID != "twice" || first.Hits.Items[0].Score != 2 || first.Hits.Items[0].Snippet != prefix || first.Counts[Messages] != 2 || first.Hits.NextCursor == "" {
		t.Fatalf("incorrect first page: %+v %v", first, err)
	}
	q.Page.Cursor = first.Hits.NextCursor
	second, err := s.Search(t.Context(), owner(), q)
	if err != nil || len(second.Hits.Items) != 1 || second.Hits.Items[0].Object.ID != "once" || second.Hits.Items[0].Score != 1 || second.Hits.Items[0].Snippet != prefix || second.Counts[Messages] != 2 || second.Hits.NextCursor != "" {
		t.Fatalf("incorrect second page: %+v %v", second, err)
	}
}

func BenchmarkSearchLongMessageSnippets(b *testing.B) {
	db, err := sqlitestore.Open(filepath.Join(b.TempDir(), "search.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	text := "needle " + strings.Repeat("界a", 16000)
	docs := make([]Document, 100)
	for i := range docs {
		docs[i] = Document{OwnerID: "owner", Source: Messages, Object: core.Object{Kind: "message", ID: core.ID(fmt.Sprintf("message-%03d", i))}, Title: "Long message", Text: text}
	}
	s := NewService(db, Options{Authorizer: fixtureAuthorizer{}, Documents: func(context.Context, core.ID) ([]Document, error) { return docs, nil }})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		result, err := s.Search(b.Context(), owner(), Query{Text: "needle", Sources: []Source{Messages}, Page: core.PageRequest{Limit: 100}})
		if err != nil || len(result.Hits.Items) != 100 {
			b.Fatalf("hits=%d error=%v", len(result.Hits.Items), err)
		}
	}
}
