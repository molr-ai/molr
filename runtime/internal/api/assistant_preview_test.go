package api

import (
	"strings"
	"testing"
)

func TestPreviewText(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"empty", "", ""},
		{"whitespace", " \t\n\u2003\u00a0", ""},
		{"normalize", "  hello\t世界\u2003🙂 \r\nnext  ", "hello 世界 🙂 next"},
		{"exact", strings.Repeat("猫", 80), strings.Repeat("猫", 80)},
		{"trailing whitespace", strings.Repeat("猫", 80) + " \t\u2003", strings.Repeat("猫", 80)},
		{"truncate", strings.Repeat("🙂", 81), strings.Repeat("🙂", 80) + "…"},
		{"space at boundary", strings.Repeat("a", 79) + " \t b", strings.Repeat("a", 79) + " …"},
		{"space after boundary", strings.Repeat("a", 80) + " \t b", strings.Repeat("a", 80) + "…"},
		{"invalid UTF-8", "hello\xff\nworld", "hello� world"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := previewText(tc.input); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

var previewResult string

func BenchmarkPreviewText(b *testing.B) {
	for _, tc := range []struct{ name, input string }{
		{"short", "  hello\t世界\u2003🙂 \r\nnext  "},
		{"long", strings.Repeat("消息 🙂 preview text\n", 2048)},
		{"long-trailing-whitespace", strings.Repeat("猫", 80) + strings.Repeat(" \t\u2003", 8192)},
	} {
		for _, implementation := range []struct {
			name string
			run  func(string) string
		}{{"current", previewText}, {"reference", referencePreviewText}} {
			b.Run(tc.name+"/"+implementation.name, func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					previewResult = implementation.run(tc.input)
				}
			})
		}
	}
}

func FuzzPreviewText(f *testing.F) {
	for _, input := range []string{"", "hello\t世界\u2003🙂", "\xff\xfe\x00\n", strings.Repeat("a", 79) + " \t b", strings.Repeat("猫", 80) + " \u00a0", strings.Repeat("🙂", 81)} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		want := referencePreviewText(input)
		if got := previewText(input); got != want {
			t.Fatalf("input %q: got %q, want %q", input, got, want)
		}
	})
}

// Reference the previous whole-message projection for equivalence and cost checks.
func referencePreviewText(text string) string {
	chars := []rune(strings.Join(strings.Fields(text), " "))
	if len(chars) > 80 {
		chars = append(chars[:80], '…')
	}
	return string(chars)
}
