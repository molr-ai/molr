// Reports timer entry points for the repository's polling review gate.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type site struct {
	File  string `json:"file"`
	Scope string `json:"scope"`
	API   string `json:"api"`
	Delay string `json:"delay"`
	Loops int    `json:"loops"`
	Line  int    `json:"line"`
}

var primitives = map[string]bool{"NewTicker": true, "Tick": true, "NewTimer": true, "After": true, "AfterFunc": true, "Sleep": true}

func syntax(n ast.Node) string {
	var b bytes.Buffer
	_ = printer.Fprint(&b, token.NewFileSet(), n)
	return b.String()
}
func scanSource(path string, source any) ([]site, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, source, 0)
	if err != nil {
		return nil, err
	}
	aliases := map[string]bool{}
	functions := map[string]string{}
	timers := map[string]bool{}
	constructors := map[string]string{}
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if p == "molr.ai/runtime/internal/im/realtime" || p == "molr.ai/runtime/internal/agent/scheduler" {
			name := filepath.Base(p)
			if imp.Name != nil {
				name = imp.Name.Name
			}
			member := "NewFeed"
			if strings.HasSuffix(p, "/scheduler") {
				member = "NewService"
			}
			key := name + "." + member
			if name == "." {
				key = member
			}
			constructors[key] = "wiring." + filepath.Base(p) + "." + member
		}
		if p == "time" {
			name := "time"
			if imp.Name != nil {
				name = imp.Name.Name
			}
			aliases[name] = true
		}
	}
	primitive := func(e ast.Expr) string {
		switch e := e.(type) {
		case *ast.SelectorExpr:
			if p, ok := e.X.(*ast.Ident); ok {
				if name := constructors[p.Name+"."+e.Sel.Name]; name != "" {
					return name
				}
			}
			if p, ok := e.X.(*ast.Ident); ok && aliases[p.Name] && primitives[e.Sel.Name] {
				return "time." + e.Sel.Name
			}
		case *ast.Ident:
			if name := constructors[e.Name]; name != "" {
				return name
			}
			if aliases["."] && primitives[e.Name] {
				return "time." + e.Name
			}
			return functions[e.Name]
		}
		return ""
	}
	// Resolve simple aliases and timer handles without depending on the host GOOS.
	for range 3 {
		ast.Inspect(f, func(n ast.Node) bool {
			assign := func(lhs, rhs ast.Expr) {
				name := syntax(lhs)
				if api := primitive(rhs); api != "" {
					functions[name] = api
				}
				if c, ok := rhs.(*ast.CallExpr); ok {
					api := primitive(c.Fun)
					if api == "time.NewTimer" || api == "time.NewTicker" || api == "time.AfterFunc" {
						timers[name] = true
					}
				}
				if timers[syntax(rhs)] {
					timers[name] = true
				}
			}
			switch n := n.(type) {
			case *ast.Field:
				if ptr, ok := n.Type.(*ast.StarExpr); ok {
					if typ, ok := ptr.X.(*ast.SelectorExpr); ok {
						if pkg, ok := typ.X.(*ast.Ident); ok && aliases[pkg.Name] && (typ.Sel.Name == "Timer" || typ.Sel.Name == "Ticker") {
							for _, name := range n.Names {
								timers[name.Name] = true
							}
						}
					}
				}
			case *ast.AssignStmt:
				for i, l := range n.Lhs {
					if i < len(n.Rhs) {
						assign(l, n.Rhs[i])
					}
				}
			case *ast.ValueSpec:
				for i, l := range n.Names {
					if i < len(n.Values) {
						assign(l, n.Values[i])
					}
				}
			}
			return true
		})
	}
	var out []site
	for _, decl := range f.Decls {
		scope := "<package>"
		if fn, ok := decl.(*ast.FuncDecl); ok {
			scope = fn.Name.Name
			if fn.Recv != nil {
				scope = "(" + syntax(fn.Recv.List[0].Type) + ")." + scope
			}
		}
		bindings := map[string][]string{}
		readBindings := func(root ast.Node) {
			ast.Inspect(root, func(n ast.Node) bool {
				add := func(lhs, rhs ast.Expr) {
					if name, ok := lhs.(*ast.Ident); ok {
						bindings[name.Name] = append(bindings[name.Name], syntax(rhs))
					}
				}
				switch n := n.(type) {
				case *ast.AssignStmt:
					for i, lhs := range n.Lhs {
						if i < len(n.Rhs) {
							add(lhs, n.Rhs[i])
						}
					}
				case *ast.ValueSpec:
					for i, lhs := range n.Names {
						if i < len(n.Values) {
							add(lhs, n.Values[i])
						}
					}
				}
				return true
			})
		}
		for _, global := range f.Decls {
			if _, ok := global.(*ast.GenDecl); ok {
				readBindings(global)
			}
		}
		readBindings(decl)
		stack := []ast.Node{}
		loops := 0
		ast.Inspect(decl, func(n ast.Node) bool {
			if n == nil {
				last := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				switch last.(type) {
				case *ast.ForStmt, *ast.RangeStmt:
					loops--
				}
				return true
			}
			stack = append(stack, n)
			switch n.(type) {
			case *ast.ForStmt, *ast.RangeStmt:
				loops++
			}
			policyField := func(lhs ast.Expr) string {
				name := ""
				switch lhs := lhs.(type) {
				case *ast.Ident:
					name = lhs.Name
				case *ast.SelectorExpr:
					name = lhs.Sel.Name
				}
				if name == "TickInterval" || name == "AuthorizationInterval" {
					return name
				}
				return ""
			}
			policy := func(lhs, rhs ast.Expr) {
				if name := policyField(lhs); name != "" {
					out = append(out, site{path, scope, "policy." + name, delayExpression(rhs, bindings), loops, fset.Position(n.Pos()).Line})
				}
			}
			switch value := n.(type) {
			case *ast.KeyValueExpr:
				policy(value.Key, value.Value)
			case *ast.AssignStmt:
				for i, lhs := range value.Lhs {
					if i < len(value.Rhs) {
						policy(lhs, value.Rhs[i])
					}
				}
			}
			if c, ok := n.(*ast.CallExpr); ok {
				api := primitive(c.Fun)
				if s, ok := c.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "Reset" && (timers[syntax(s.X)] || func() bool { field, ok := s.X.(*ast.SelectorExpr); return ok && timers[field.Sel.Name] }()) {
					api = "timer.Reset"
				}
				if api != "" {
					delay := ""
					if len(c.Args) > 0 {
						delay = delayExpression(c.Args[0], bindings)
					}
					if strings.HasPrefix(api, "wiring.") {
						delay = wiring(c, api)
					}
					out = append(out, site{path, scope, api, delay, loops, fset.Position(c.Pos()).Line})
				}
			} else if e, ok := n.(ast.Expr); ok {
				if api := primitive(e); api != "" {
					if len(stack) > 1 {
						parent := stack[len(stack)-2]
						switch p := parent.(type) {
						case *ast.ValueSpec:
							for _, name := range p.Names {
								if name == e {
									return true
								}
							}
						case *ast.AssignStmt:
							for _, lhs := range p.Lhs {
								if lhs == e {
									return true
								}
							}
						}
						if p, ok := parent.(*ast.CallExpr); ok && p.Fun == e {
							return true
						}
					}
					out = append(out, site{path, scope, "reference:" + api, "", loops, fset.Position(n.Pos()).Line})
				}
			}
			return true
		})
	}
	return out, nil
}
func wiring(call *ast.CallExpr, api string) string {
	required := []string{"WatchAuthorization"}
	if api == "wiring.scheduler.NewService" {
		required = []string{"TickSignals", "NextTick"}
	}
	found := map[string]bool{}
	if len(call.Args) == 2 {
		if options, ok := call.Args[1].(*ast.CompositeLit); ok {
			for _, elt := range options.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					valid := syntax(kv.Value) != "nil"
					if values, ok := kv.Value.(*ast.CompositeLit); ok && len(values.Elts) == 0 {
						valid = false
					}
					found[syntax(kv.Key)] = valid
				}
			}
		}
	}
	for _, key := range required {
		if !found[key] {
			return "missing:" + key
		}
	}
	return strings.Join(required, "+")
}

func delayExpression(expr ast.Expr, bindings map[string][]string) string {
	seen := map[string]bool{}
	var inspect func(ast.Expr)
	inspect = func(expr ast.Expr) {
		ast.Inspect(expr, func(n ast.Node) bool {
			if name, ok := n.(*ast.Ident); ok && !seen[name.Name] && len(bindings[name.Name]) > 0 {
				seen[name.Name] = true
				for _, value := range bindings[name.Name] {
					if next, err := parser.ParseExpr(value); err == nil {
						inspect(next)
					}
				}
			}
			return true
		})
	}
	inspect(expr)
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	text := syntax(expr)
	for _, name := range names {
		text += " [" + name + "=" + strings.Join(bindings[name], "; ") + "]"
	}
	return text
}

func collect(root string) ([]site, error) {
	var out []site
	for _, part := range []string{"runtime/internal", "runtime/cmd"} {
		err := filepath.WalkDir(filepath.Join(root, part), func(path string, d fs.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rows, err := scanSource(filepath.ToSlash(rel), data)
			out = append(out, rows...)
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}
func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: polling-go <repository>")
		os.Exit(2)
	}
	rows, err := collect(os.Args[1])
	if err == nil {
		err = json.NewEncoder(os.Stdout).Encode(rows)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
