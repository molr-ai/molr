package textfiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkFileSearchShortLines(b *testing.B) {
	for _, fixture := range []struct {
		name    string
		pattern string
		context int
	}{
		{"first_match", "needle", 0},
		{"first_match_with_context", "needle", 5},
		{"no_match", "missing", 0},
	} {
		b.Run(fixture.name, func(b *testing.B) {
			root := b.TempDir()
			data := "needle\n" + strings.Repeat("x\n", ((2<<20)-len("needle\n"))/2)
			if err := os.WriteFile(filepath.Join(root, "short-lines.txt"), []byte(data), 0600); err != nil {
				b.Fatal(err)
			}
			options := SearchOptions{Pattern: fixture.pattern, CaseSensitive: true, ContextLines: fixture.context, MaxResults: 1}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				result, err := Search(b.Context(), root, "short-lines.txt", options, func(string) error { return nil })
				if err != nil || result.BytesScanned != len(data) {
					b.Fatal(result, err)
				}
				if fixture.pattern == "needle" {
					if len(result.Matches) != 1 || result.Matches[0].Line != 1 || len(result.Matches[0].After) != fixture.context || result.StopReason != "result_limit" {
						b.Fatal(result)
					}
				} else if len(result.Matches) != 0 || !result.Complete {
					b.Fatal(result)
				}
			}
		})
	}
}
