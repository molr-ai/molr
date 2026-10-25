package architecture

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// These boundaries keep IM reusable without coupling it to assistant execution
// and prevent domain code from importing HTTP or concrete persistence adapters.
func TestPackageDependencyBoundaries(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "../.."))
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		source, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range source.Imports {
			imported, _ := strconv.Unquote(spec.Path.Value)
			if strings.HasPrefix(rel, "internal/im/") && (strings.Contains(imported, "/internal/agent") || strings.Contains(imported, "/internal/business")) {
				t.Errorf("IM imports product execution: %s -> %s", rel, imported)
			}
			if strings.HasPrefix(rel, "internal/business/") && (strings.Contains(imported, "/internal/storage") || strings.Contains(imported, "/internal/api") || strings.Contains(imported, "/internal/agent")) {
				t.Errorf("domain imports adapter/execution: %s -> %s", rel, imported)
			}
			if strings.HasPrefix(rel, "internal/trigger/") && (strings.Contains(imported, "/internal/business") || strings.Contains(imported, "/internal/agent") || strings.Contains(imported, "/internal/im") || strings.Contains(imported, "/internal/api") || strings.Contains(imported, "/internal/storage")) {
				t.Errorf("shared trigger imports business or execution: %s -> %s", rel, imported)
			}
			if strings.HasPrefix(rel, "internal/shellsession/") && (strings.Contains(imported, "/internal/api") || strings.Contains(imported, "/internal/platform/sandbox") || strings.Contains(imported, "/internal/sandboxmanager")) {
				t.Errorf("session manager imports concrete host adapter: %s -> %s", rel, imported)
			}
			if (strings.HasPrefix(rel, "internal/filetools/") || strings.HasPrefix(rel, "internal/workspace/") || strings.HasPrefix(rel, "internal/sandboxruntime/")) && strings.Contains(imported, "/internal/api") {
				t.Errorf("runtime implementation imports HTTP adapter: %s -> %s", rel, imported)
			}
			if !strings.HasPrefix(rel, "internal/app/") && strings.Contains(imported, "/internal/app") {
				t.Errorf("reverse composition dependency: %s", rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
