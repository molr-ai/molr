package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestFullBucketScansHaveExplicitWholeScopePurpose(t *testing.T) {
	allowed := map[string]string{
		"im/channelworker/worker.go:initial":         "one startup snapshot restores observers for all current bindings",
		"im/channelworker/worker.go:pending":         "drains only committed binding discoveries, each of which needs an observer",
		"auth/administration.go:checkAccountStorage": "startup validates every persisted account and group binding",
		"auth/administration.go:Groups":              "administrator explicitly requests the complete group catalog",
		"debugtrace/retention.go:SetRetention":       "installation-wide settings invalidate every registered workbench",
		"sandboxruntime/waits.go:Tick":               "collection contains only pending waits; all require probing",
		"app/data.go:collectFiles":                   "collection contains only pending file deletions; every entry requires an attempt",
		"persistence/store.go:Scan":                  "the storage primitive itself",
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
			for _, call := range wholeReads(file, f) {
				key := filepath.ToSlash(rel) + ":" + f.Name.Name
				if allowed[key] == "" {
					t.Errorf("unreviewed full-bucket read: %s (%s)", key, call)
				}
				seen[key] = true
			}
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

type queryScope uint8

const (
	unknown queryScope = iota
	whole
	bounded
)

// This is a local syntactic guard, not an SQL cost model. The behavioral tests
// also count idle database accesses, including filtered queries and helper calls.
func wholeReads(file *ast.File, f *ast.FuncDecl) []string {
	aliases := map[string]bool{}
	for _, imp := range file.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if path == "molr.ai/runtime/internal/persistence" {
			name := "persistence"
			if imp.Name != nil {
				name = imp.Name.Name
			}
			aliases[name] = true
		}
	}

	vars := map[string]map[string]bool{}
	nonempty := func(e ast.Expr) bool {
		switch v := e.(type) {
		case *ast.Ident:
			return v.Name != "nil"
		case *ast.BasicLit:
			return v.Value != "0" && v.Value != `""`
		case *ast.CompositeLit:
			return len(v.Elts) > 0
		}
		return true
	}
	queryTypes := map[string]bool{"Query": aliases["."]}
	isQuery := func(e ast.Expr) bool {
		if id, ok := e.(*ast.Ident); ok {
			return queryTypes[id.Name]
		}
		typ, ok := e.(*ast.SelectorExpr)
		if !ok || typ.Sel.Name != "Query" {
			return false
		}
		pkg, ok := typ.X.(*ast.Ident)
		return ok && aliases[pkg.Name]
	}
	for _, decl := range file.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok {
			for _, spec := range gen.Specs {
				if typ, ok := spec.(*ast.TypeSpec); ok && isQuery(typ.Type) {
					queryTypes[typ.Name.Name] = true
				}
			}
		}
	}
	var shape func(ast.Expr) map[string]bool
	shape = func(e ast.Expr) map[string]bool {
		switch v := e.(type) {
		case *ast.Ident:
			original, ok := vars[v.Name]
			if !ok || original == nil {
				return nil
			}
			copy := map[string]bool{}
			for k, v := range original {
				copy[k] = v
			}
			return copy
		case *ast.UnaryExpr:
			return shape(v.X)
		case *ast.ParenExpr:
			return shape(v.X)
		case *ast.CompositeLit:
			if !isQuery(v.Type) {
				return nil
			}
			fields := map[string]bool{}
			for _, elt := range v.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					if key, ok := kv.Key.(*ast.Ident); ok {
						fields[key.Name] = nonempty(kv.Value)
					}
				}
			}
			return fields
		}
		return nil
	}
	scope := func(e ast.Expr) queryScope {
		fields := shape(e)
		if fields == nil {
			return unknown
		}
		for _, key := range []string{"Where", "Any", "Prefix", "Limit", "After"} {
			if fields[key] {
				return bounded
			}
		}
		return whole
	}
	var result []string
	ast.Inspect(f.Body, func(n ast.Node) bool {
		switch n := n.(type) {

		case *ast.ValueSpec:
			for i, name := range n.Names {
				if i < len(n.Values) {
					vars[name.Name] = shape(n.Values[i])
				} else if isQuery(n.Type) {
					vars[name.Name] = map[string]bool{}
				}
			}
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				if i >= len(n.Rhs) {
					continue
				}
				switch lhs := lhs.(type) {
				case *ast.Ident:
					vars[lhs.Name] = shape(n.Rhs[i])
				case *ast.SelectorExpr:
					if name, ok := lhs.X.(*ast.Ident); ok && vars[name.Name] != nil {
						vars[name.Name][lhs.Sel.Name] = nonempty(n.Rhs[i])
					}
				}
			}
		case *ast.CallExpr:
			expr := n.Fun
			switch x := expr.(type) {
			case *ast.IndexExpr:
				expr = x.X
			case *ast.IndexListExpr:
				expr = x.X
			}
			sel, ok := expr.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ident := sel.X.(*ast.Ident)
			persist := ident && aliases[pkg.Name]
			switch {
			case sel.Sel.Name == "Scan" && (persist || len(n.Args) == 2 && !addressArgument(n.Args[0])):
				result = append(result, "Scan")
			case sel.Sel.Name == "Select" && persist && len(n.Args) == 4:
				if scope(n.Args[3]) == whole {
					result = append(result, "Select")
				}
			case sel.Sel.Name == "Query" && len(n.Args) == 3:
				if scope(n.Args[2]) == whole {
					result = append(result, "Query")
				}
			}
		}
		return true
	})
	return result
}

func TestWholeReadGuardRecognizesEquivalentQueries(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"scan alias", `p.Scan[Row](ctx,store,bucket)`, 1},
		{"renamed transaction", `transaction.Scan(ctx,bucket)`, 1},
		{"empty select", `p.Select[Row](ctx,store,bucket,p.Query{})`, 1},
		{"empty transaction query", `transaction.Query(ctx,bucket,p.Query{})`, 1},
		{"sort is not a filter", `p.Select[Row](ctx,store,bucket,p.Query{OrderBy:"key",Unordered:false})`, 1},
		{"zero limits and empty filters", `p.Select[Row](ctx,store,bucket,p.Query{Limit:0,Where:[]p.Filter{},Prefix:""})`, 1},
		{"local variable", `q:=p.Query{}; p.Select[Row](ctx,store,bucket,q)`, 1},
		{"variable alias", `q:=p.Query{}; alias:=q; transaction.Query(ctx,bucket,alias)`, 1},
		{"cleared filter", `q:=p.Query{Where:[]p.Filter{p.Eq("State","pending")}};q.Where=nil;transaction.Query(ctx,bucket,q)`, 1},
		{"zero limit assignment", `q:=p.Query{};q.Limit=0;transaction.Query(ctx,bucket,q)`, 1},
		{"retained filter", `q:=p.Query{Where:[]p.Filter{p.Eq("State","pending")}};q.Limit=0;transaction.Query(ctx,bucket,q)`, 0},
		{"copied scope", `q:=p.Query{};alias:=q;q.Limit=1;transaction.Query(ctx,bucket,alias)`, 1},
		{"zero value", `var q p.Query; transaction.Query(ctx,bucket,q)`, 1},
		{"filtered", `p.Select[Row](ctx,store,bucket,p.Query{Where:[]p.Filter{p.Eq("State","pending")}})`, 0},
		{"bounded", `transaction.Query(ctx,bucket,p.Query{Limit:1})`, 0},
		{"assigned filter", `q:=p.Query{};q.Where=[]p.Filter{p.Eq("BindingID",id)};transaction.Query(ctx,bucket,q)`, 0},
		{"SQL row decoding", `rows.Scan(&key,&data)`, 0},
		{"ordinary scanner", `scanner.Scan()`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", `package fixture;import p "molr.ai/runtime/internal/persistence";func example(){`+tc.body+`}`, 0)
			if err != nil {
				t.Fatal(err)
			}
			f := file.Decls[1].(*ast.FuncDecl)
			if got := wholeReads(file, f); len(got) != tc.want {
				t.Fatalf("got %v; want %d reads", got, tc.want)
			}
		})
	}
}

func addressArgument(e ast.Expr) bool { u, ok := e.(*ast.UnaryExpr); return ok && u.Op == token.AND }
