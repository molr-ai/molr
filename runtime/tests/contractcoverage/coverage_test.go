// Package contractcoverage checks the public-contract test plan against Go declarations.
// It measures inventory completeness, not executed paths or behavioral coverage.
package contractcoverage

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type sourceType struct {
	expression ast.Expr
	imports    map[string]string
}

type packageInventory struct {
	types   map[string]sourceType
	public  map[string]bool
	tests   map[string]bool
	covered map[string]bool
}

func TestPublicContractCoverageInventory(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate Runtime source tree")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "../.."))
	packages, err := scanSources(root)
	if err != nil {
		t.Fatal(err)
	}
	for path, pkg := range packages {
		for name, typ := range pkg.types {
			if !ast.IsExported(name) {
				continue
			}
			if _, ok := typ.expression.(*ast.InterfaceType); !ok {
				continue
			}
			methods, err := interfaceMethods(packages, path, name, map[string]bool{})
			if err != nil {
				t.Fatal(err)
			}
			for method := range methods {
				if ast.IsExported(method) {
					pkg.public[name+"."+method] = true
				}
			}
		}
	}

	for _, group := range []string{"business", "im-agent", "security", "infrastructure"} {
		path := filepath.Join(root, "tests", "coverage-"+group+".md")
		if err := readMapping(t, path, packages); err != nil {
			t.Errorf("%s: %v", filepath.Base(path), err)
		}
	}
	paths := make([]string, 0, len(packages))
	for path := range packages {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	total, mapped := 0, 0
	for _, path := range paths {
		pkg := packages[path]
		var missing []string
		for entry := range pkg.public {
			total++
			if pkg.covered[entry] {
				mapped++
			} else {
				missing = append(missing, entry)
			}
		}
		sort.Strings(missing)
		if len(missing) != 0 {
			t.Errorf("%s: missing public contract mappings: %s", path, strings.Join(missing, ", "))
		}
	}
	t.Logf("public contract inventory: %d/%d entry points mapped across %d packages; this is not execution coverage", mapped, total, len(packages))
}

func scanSources(root string) (map[string]*packageInventory, error) {
	packages := map[string]*packageInventory{}
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		packagePath := filepath.ToSlash(rel)
		pkg := packages[packagePath]
		if pkg == nil {
			pkg = &packageInventory{types: map[string]sourceType{}, public: map[string]bool{}, tests: map[string]bool{}, covered: map[string]bool{}}
			packages[packagePath] = pkg
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		isTest := strings.HasSuffix(path, "_test.go")
		imports := map[string]string{}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			name := filepath.Base(importPath)
			if spec.Name != nil {
				name = spec.Name.Name
			}
			imports[name] = strings.TrimPrefix(importPath, "molr.ai/runtime/")
		}
		for _, declaration := range file.Decls {
			switch decl := declaration.(type) {
			case *ast.FuncDecl:
				if isTest {
					if decl.Recv == nil && strings.HasPrefix(decl.Name.Name, "Test") && testSignature(decl.Type) {
						pkg.tests[decl.Name.Name] = true
					}
					continue
				}
				if !ast.IsExported(decl.Name.Name) {
					continue
				}
				name := decl.Name.Name
				if decl.Recv != nil {
					receiver := typeName(decl.Recv.List[0].Type)
					if !ast.IsExported(receiver) {
						continue
					}
					name = receiver + "." + name
				}
				pkg.public[name] = true
			case *ast.GenDecl:
				if isTest || decl.Tok != token.TYPE {
					continue
				}
				for _, spec := range decl.Specs {
					typ := spec.(*ast.TypeSpec)
					pkg.types[typ.Name.Name] = sourceType{expression: typ.Type, imports: imports}
				}
			}
		}
		return nil
	})
	return packages, err
}

func typeName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return typeName(value.X)
	case *ast.IndexExpr:
		return typeName(value.X)
	case *ast.IndexListExpr:
		return typeName(value.X)
	}
	return ""
}

func testSignature(function *ast.FuncType) bool {
	if function.Params == nil || len(function.Params.List) != 1 || function.Results != nil {
		return false
	}
	parameter := function.Params.List[0]
	if len(parameter.Names) > 1 {
		return false
	}
	pointer, ok := parameter.Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	selector, ok := pointer.X.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "T"
}

// Expand inherited methods under each exported interface's own name so, e.g.,
// mapping Client.Get alone cannot silently omit the public Service.Get entry.
func interfaceMethods(packages map[string]*packageInventory, path, name string, stack map[string]bool) (map[string]bool, error) {
	key := path + "." + name
	if stack[key] {
		return nil, fmt.Errorf("cyclic embedded interface: %s", key)
	}
	stack[key] = true
	defer delete(stack, key)
	if path == "io" && name == "ReadWriteCloser" {
		methods := map[string]bool{}
		typ := reflect.TypeOf((*io.ReadWriteCloser)(nil)).Elem()
		for i := 0; i < typ.NumMethod(); i++ {
			methods[typ.Method(i).Name] = true
		}
		return methods, nil
	}
	pkg := packages[path]
	if pkg == nil {
		return nil, fmt.Errorf("embedded interface package outside inventory: %s", key)
	}
	typ, ok := pkg.types[name]
	if !ok {
		return nil, fmt.Errorf("unknown embedded interface: %s", key)
	}
	inter, ok := typ.expression.(*ast.InterfaceType)
	if !ok {
		if alias, ok := typ.expression.(*ast.Ident); ok {
			return interfaceMethods(packages, path, alias.Name, stack)
		}
		return nil, fmt.Errorf("embedded type is not an interface: %s", key)
	}
	methods := map[string]bool{}
	for _, field := range inter.Methods.List {
		if len(field.Names) != 0 {
			for _, method := range field.Names {
				methods[method.Name] = true
			}
			continue
		}
		embeddedPath, embeddedName := path, ""
		switch embedded := field.Type.(type) {
		case *ast.Ident:
			embeddedName = embedded.Name
		case *ast.SelectorExpr:
			qualifier, ok := embedded.X.(*ast.Ident)
			if !ok {
				return nil, fmt.Errorf("unsupported embedded interface qualifier in %s", key)
			}
			embeddedPath, embeddedName = typ.imports[qualifier.Name], embedded.Sel.Name
		default:
			return nil, fmt.Errorf("unsupported embedded interface expression %T in %s", field.Type, key)
		}
		inherited, err := interfaceMethods(packages, embeddedPath, embeddedName, stack)
		if err != nil {
			return nil, err
		}
		for method := range inherited {
			methods[method] = true
		}
	}
	return methods, nil
}

var codeSpan = regexp.MustCompile("`([^`]+)`")
var publicTestName = regexp.MustCompile(`(?:internal/[A-Za-z0-9_/-]+\.)?Test[A-Za-z0-9_]+\b`)

func readMapping(t *testing.T, path string, packages map[string]*packageInventory) error {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	line := 0
	for scanner.Scan() {
		line++
		row := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(row, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(row, "|"), "|")
		if len(cells) != 3 {
			continue // Introductory tables may use a different format.
		}
		packagePath := strings.Trim(strings.TrimSpace(cells[0]), "`")
		if !strings.HasPrefix(packagePath, "internal/") {
			continue // Header/separator or another documentation table.
		}
		location := fmt.Sprintf("%s:%d", filepath.Base(path), line)
		pkg := packages[packagePath]
		if pkg == nil {
			t.Errorf("%s: unknown package %q", location, packagePath)
			continue
		}
		methods := codeSpan.FindAllStringSubmatch(cells[1], -1)
		if len(methods) == 0 {
			for _, method := range strings.FieldsFunc(cells[1], func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\t' }) {
				methods = append(methods, []string{"", method})
			}
		}
		tests := publicTestName.FindAllString(cells[2], -1)
		validTests := len(tests) > 0
		if !validTests {
			t.Errorf("%s: mapping references no concrete test", location)
		}
		for _, test := range tests {
			testPackage, testName := pkg, test
			if separator := strings.LastIndex(test, "."); separator >= 0 {
				testPackage = packages[test[:separator]]
				testName = test[separator+1:]
			}
			if testPackage == nil || !testPackage.tests[testName] {
				t.Errorf("%s: %s does not define test %s", location, packagePath, test)
				validTests = false
			}
		}
		for _, match := range methods {
			method := strings.TrimSpace(match[1])
			// Allow qualified functions such as core.NewSecret as well as
			// NewSecret, while retaining names such as Secret.MarshalJSON.
			method = strings.TrimPrefix(method, filepath.Base(packagePath)+".")
			if !pkg.public[method] {
				t.Errorf("%s: %s has no public entry point %s", location, packagePath, method)
				continue
			}
			if validTests {
				pkg.covered[method] = true
			}
		}
	}
	return scanner.Err()
}
