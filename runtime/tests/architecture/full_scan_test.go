package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestFullBucketScansHaveExplicitWholeScopePurpose(t *testing.T) {
	// These operations intentionally consume their entire collection. Every new
	// full-bucket read must document why a narrower query cannot do the job.
	allowed := map[string]string{
		"auth/administration.go:checkAccountStorage": "startup validates every persisted account and group binding",
		"auth/administration.go:Groups":              "administrator explicitly requests the complete group catalog",
		"debugtrace/retention.go:SetRetention":       "installation-wide settings invalidate every registered workbench",
		"app/remote_waits.go:pollRemoteWaits":        "collection contains only pending waits; all require probing",
		"app/data.go:collectFiles":                   "collection contains only pending file deletions; every entry requires an attempt",
		"persistence/store.go:Scan":                  "the primitive itself, not an additional business read",
	}
	seen := map[string]bool{}
	root := filepath.Join("..", "..", "internal")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, decl := range file.Decls {
			f, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				expr := call.Fun
				if generic, ok := expr.(*ast.IndexExpr); ok {
					expr = generic.X
				}
				selector, ok := expr.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "Scan" {
					return true
				}
				receiver, ok := selector.X.(*ast.Ident)
				if !ok || receiver.Name != "tx" && receiver.Name != "persistence" {
					return true
				}
				key := filepath.ToSlash(rel) + ":" + f.Name.Name
				if allowed[key] == "" {
					t.Errorf("unreviewed full-bucket read: %s", key)
				}
				seen[key] = true
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for key := range allowed {
		if !seen[key] {
			t.Errorf("remove stale scan allowance: %s", key)
		}
	}
}
