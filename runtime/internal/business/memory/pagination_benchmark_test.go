package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/core"
)

// Measure a 50-item memory page without model calls or browser rendering.
func BenchmarkMemoryFirstPage(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			root := b.TempDir()
			directory := filepath.Join(root, ".agents", "memory")
			if err := os.MkdirAll(directory, 0700); err != nil {
				b.Fatal(err)
			}
			body := []byte(strings.Repeat("Memory content. ", 256))
			for i := 0; i < count; i++ {
				if err := os.WriteFile(filepath.Join(directory, fmt.Sprintf("note-%04d.md", i)), body, 0600); err != nil {
					b.Fatal(err)
				}
			}
			files := NewFiles(FileOptions{Roots: func(context.Context, core.ID, RootQuery) ([]Root, error) {
				return []Root{{Scope: Assistant, AssistantID: "assistant", Directory: root}}, nil
			}})
			query := Query{Scope: Assistant, AssistantID: "assistant", Page: core.PageRequest{Limit: 50}}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				page, err := files.List(b.Context(), "owner", query)
				if err != nil {
					b.Fatal(err)
				}
				if len(page.Items) != 50 || page.NextCursor == "" {
					b.Fatal("invalid first page")
				}
			}
		})
	}
}
