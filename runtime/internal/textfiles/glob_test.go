package textfiles

import (
	"context"
	"molr.ai/runtime/internal/core"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCompiledGlobsMatchRecursiveSemantics(t *testing.T) {
	patterns := []string{"", "*", "**", "**/**", "**/*.go", "src/**/test?.[gt]o", "a/**/b/**/c", "**/a/*/b", "a/**/**/b", "**/[!a-c]*", "**/[^a-c]*", "**/文件?.md", "**/*[界文]*/**", "a//b", "/a/**/", "**/**/a/**/**/z", "[", "a/**/"}
	names := []string{"", "a", "a/b", "a/b/c", "a/b/b/x/c", "x/a/x/a/x/b", "src/test1.go", "src/a/b/test2.to", "src/test1.go/child", "文件甲.md", "中文/文件乙.md", "a//b", "/a/", "a/", "x/世界/y/z"}
	for _, pattern := range patterns {
		compiled := compileGlobs([]string{pattern})[0]
		for _, name := range names {
			got := compiled.matches(strings.Split(name, "/"))
			want := legacyGlobMatch(pattern, name)
			if got != want {
				t.Fatalf("pattern %q name %q got %v want %v", pattern, name, got, want)
			}
		}
	}
}

func TestFileSearchGlobsRetainExclusionsAndAuthorization(t *testing.T) {
	root := t.TempDir()
	paths := []string{"main.go", "src/a.go", "src/nested/b.go", "src/vendor/private.go", "vendor/top.go", "docs/readme.md", "src/.hidden.go", "src/文件甲.go"}
	for _, name := range paths {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte("before\nneedle\nafter\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := Search(t.Context(), root, ".", SearchOptions{Pattern: "needle", Glob: []string{"**/*.go"}, ExcludeGlob: []string{"**/vendor/**"}, ContextLines: 1}, func(name string) error {
		if strings.Contains(name, "vendor") {
			t.Fatal("excluded directory reached authorization", name)
		}
		return nil
	})
	if err != nil || !result.Complete {
		t.Fatal(result, err)
	}
	got := []string{}
	for _, m := range result.Matches {
		got = append(got, m.Path)
		if m.Line != 2 || len(m.Before) != 1 || len(m.After) != 1 {
			t.Fatal(m)
		}
	}
	want := []string{"main.go", "src/a.go", "src/nested/b.go", "src/文件甲.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got, want)
	}
	result, err = Search(t.Context(), root, "src/a.go", SearchOptions{Mode: "files", Glob: []string{"*.go"}, ExcludeGlob: []string{"a.go"}}, func(string) error { return nil })
	if err != nil || len(result.Matches) != 0 || result.FilesScanned != 0 {
		t.Fatal("single-file exclusion changed", result, err)
	}
	_, err = Search(t.Context(), root, ".", SearchOptions{Mode: "files", Glob: []string{"**/*.go"}}, func(name string) error {
		if name == "src/a.go" {
			return core.NewError(core.Forbidden, "revoked")
		}
		return nil
	})
	if core.CodeOf(err) != core.Forbidden {
		t.Fatal("authorization bypass", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = Search(ctx, root, ".", SearchOptions{Mode: "files", Glob: []string{"**/*.go"}}, func(string) error { return nil })
	if core.CodeOf(err) != core.Canceled {
		t.Fatal(err)
	}
}

func FuzzCompiledGlobMatchesRecursive(f *testing.F) {
	for _, pair := range [][2]string{{"**/a/**/b", "x/a/y/a/z/b"}, {"**/*.go", "src/文件.go"}, {"a/**/**/b", "a/b"}, {"**/a/*/b", "a/x/a/y/b"}, {"a//b", "a//b"}, {"[", "a"}} {
		f.Add(pair[0], pair[1])
	}
	f.Fuzz(func(t *testing.T, pattern, name string) {
		if len(pattern) > 256 || len(name) > 256 {
			return
		}
		got := compileGlobs([]string{pattern})[0].matches(strings.Split(name, "/"))
		want := legacyGlobMatch(pattern, name)
		if got != want {
			t.Fatalf("%q %q: %v != %v", pattern, name, got, want)
		}
	})
}
