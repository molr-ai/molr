package textfiles

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSearchLineBoundariesAndContext(t *testing.T) {
	long := strings.Repeat("x", 4097)
	for _, tc := range []struct {
		name, data, pattern string
		context, limit      int
		matches             []SearchMatch
		longLines           int
		stop                string
	}{
		{name: "empty", pattern: "^$"},
		{name: "blank lines", data: "\n\n", pattern: "^$", context: 5, matches: []SearchMatch{
			{Line: 1, After: []SearchLine{{2, ""}}}, {Line: 2, Before: []SearchLine{{1, ""}}},
		}},
		{name: "unterminated", data: "before\nneedle", pattern: "needle", context: 5, matches: []SearchMatch{
			{Line: 2, Text: "needle", Before: []SearchLine{{1, "before"}}},
		}},
		{name: "CRLF", data: "before\r\nneedle\r\nafter\r\n", pattern: "needle", context: 5, matches: []SearchMatch{
			{Line: 2, Text: "needle", Before: []SearchLine{{1, "before"}}, After: []SearchLine{{3, "after"}}},
		}},
		{name: "first match still has context", data: "needle\n\n尾行\n", pattern: "needle", context: 5, limit: 1, stop: "result_limit", matches: []SearchMatch{
			{Line: 1, Text: "needle", After: []SearchLine{{2, ""}, {3, "尾行"}}},
		}},
		{name: "context window wraps", data: "0\n1\n2\n3\n4\n5\n6\nneedle\n8\n9\n10\n11\n12\n13\nneedle", pattern: "needle", context: 5, matches: []SearchMatch{
			{Line: 8, Text: "needle", Before: []SearchLine{{3, "2"}, {4, "3"}, {5, "4"}, {6, "5"}, {7, "6"}}, After: []SearchLine{{9, "8"}, {10, "9"}, {11, "10"}, {12, "11"}, {13, "12"}}},
			{Line: 15, Text: "needle", Before: []SearchLine{{10, "9"}, {11, "10"}, {12, "11"}, {13, "12"}, {14, "13"}}},
		}},
		{name: "long context keeps line numbers", data: "before\n" + long + "\nneedle\n" + long + "\nafter", pattern: "needle", context: 2, longLines: 2, matches: []SearchMatch{
			{Line: 3, Text: "needle", Before: []SearchLine{{1, "before"}}, After: []SearchLine{{5, "after"}}},
		}},
		{name: "unvisited long context does not change statistics", data: "needle\n" + long + "\nafter", pattern: "needle", context: 2, limit: 1, stop: "result_limit", matches: []SearchMatch{
			{Line: 1, Text: "needle", After: []SearchLine{{3, "after"}}},
		}},
		{name: "CR length boundary", data: strings.Repeat("x", 4096) + "\r\nneedle", pattern: "needle", context: 1, matches: []SearchMatch{{Line: 2, Text: "needle"}}},
		{name: "zero context", data: "前文\nneedle\n后文", pattern: "needle", matches: []SearchMatch{{Line: 2, Text: "needle"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "fixture.txt"), []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			result, err := Search(t.Context(), root, "fixture.txt", SearchOptions{Pattern: tc.pattern, Regex: true, ContextLines: tc.context, MaxResults: tc.limit}, func(string) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			for i := range result.Matches {
				if result.Matches[i].Path != "fixture.txt" || result.Matches[i].Version == "" {
					t.Fatal("missing file identity", result.Matches[i])
				}
				result.Matches[i].Path, result.Matches[i].Version = "", ""
			}
			if len(result.Matches) != len(tc.matches) || (len(tc.matches) > 0 && !reflect.DeepEqual(result.Matches, tc.matches)) {
				t.Fatalf("matches = %#v, want %#v", result.Matches, tc.matches)
			}
			if result.Skipped["long_line"] != tc.longLines || result.StopReason != tc.stop || result.Truncated != (tc.stop != "") || result.Complete != (tc.longLines == 0 && tc.stop == "") || result.BytesScanned != len(tc.data) {
				t.Fatalf("unexpected scan status: %+v", result)
			}
		})
	}
}

func TestSearchValidatesWholeFileBeforeEarlyMatch(t *testing.T) {
	for _, suffix := range []string{"\x00", "\xff"} {
		root := t.TempDir()
		data := "needle\n" + suffix
		if err := os.WriteFile(filepath.Join(root, "fixture.txt"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		for _, mode := range []string{"content", "files"} {
			result, err := Search(t.Context(), root, "fixture.txt", SearchOptions{Pattern: "needle", Mode: mode, MaxResults: 1}, func(string) error { return nil })
			if err != nil || len(result.Matches) != 0 || result.Skipped["binary"] != 1 || !result.Complete || result.Truncated {
				t.Fatal(result, err)
			}
		}
	}
}
