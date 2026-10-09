package textfiles

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkFileSearchGlobFiltering(b *testing.B) {
	for _, size := range []int{2000, 8000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			root := b.TempDir()
			for i := 0; i < size; i++ {
				dir := filepath.Join(root, fmt.Sprintf("package-%03d", i/100), "nested", "src")
				if i%100 == 0 {
					if err := os.MkdirAll(dir, 0700); err != nil {
						b.Fatal(err)
					}
				}
				if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file-%04d.txt", i)), []byte("fixture\n"), 0600); err != nil {
					b.Fatal(err)
				}
			}
			options := SearchOptions{Mode: "files", Glob: []string{"**/api/**/*.go", "**/config/**/*.json", "**/tasks/**/*.yaml", "**/src/**/*.ts", "**/web/**/*.tsx", "**/docs/**/*.md", "**/test/**/*.spec.ts", "**/generated/**/*.proto"}, ExcludeGlob: []string{"**/vendor/**", "**/node_modules/**", "**/cache/**", "**/build/**"}}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				result, err := Search(b.Context(), root, ".", options, func(string) error { return nil })
				if err != nil || !result.Complete || len(result.Matches) != 0 || result.FilesScanned != 0 || result.EntriesScanned < size {
					b.Fatal(result, err)
				}
			}
		})
	}
}
