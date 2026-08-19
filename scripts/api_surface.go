// Command api_surface prints the exported API surface of every top-level
// package directory in this module. Output is deterministic; scripts/
// check_api.py diffs it against the locks in api/. Run `make api-update`
// to accept a deliberate surface change.
package main

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// alwaysBuildTags names every build tag this module gates a file
// behind, today only ledger_sqlite (ledger/sqlite_store.go). The lock
// tool always includes a tag-gated file's exported surface, so
// api/*.txt captures the symbol regardless of tag; only the real
// `go build` and `go test` invocations honor the tag and keep the
// dependency out of the default binary. Extend this list when a
// future package adds its own gated file.
var alwaysBuildTags = []string{"ledger_sqlite"}

func main() {
	build.Default.BuildTags = alwaysBuildTags
	dirs, err := packageDirs()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, dir := range dirs {
		lines, err := surface(dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("package " + filepath.Base(dir))
		for _, line := range lines {
			fmt.Println("  " + line)
		}
	}
}

// packageDirs lists top-level directories that hold non-test Go files.
func packageDirs() ([]string, error) {
	entries, err := os.ReadDir(".")
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Name() == "scripts" {
			continue
		}
		files, err := filepath.Glob(filepath.Join(e.Name(), "*.go"))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if !strings.HasSuffix(f, "_test.go") {
				dirs = append(dirs, e.Name())
				break
			}
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

// surface returns the sorted exported-symbol blocks of the package in
// dir. A struct type is one multi-line block so its fields stay attached.
// A file excluded by a build constraint not in alwaysBuildTags never
// enters the surface; GOOS/GOARCH constraints still apply normally.
func surface(dir string) ([]string, error) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	pkgs := make(map[string]*ast.Package)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		match, err := build.Default.MatchFile(dir, e.Name())
		if err != nil {
			return nil, err
		}
		if !match {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			return nil, err
		}
		pkg := pkgs[f.Name.Name]
		if pkg == nil {
			pkg = &ast.Package{Name: f.Name.Name, Files: make(map[string]*ast.File)}
			pkgs[f.Name.Name] = pkg
		}
		pkg.Files[e.Name()] = f
	}
	want := filepath.Base(dir)
	if len(pkgs) != 1 {
		return nil, fmt.Errorf("%s: holds %d packages; exactly one package named %q is required", dir, len(pkgs), want)
	}
	for name := range pkgs {
		if name != want {
			return nil, fmt.Errorf("%s: package %q does not match directory name %q", dir, name, want)
		}
	}
	var blocks []string
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				blocks = appendDecl(blocks, fset, decl)
			}
		}
	}
	sort.Strings(blocks)
	return blocks, nil
}

// appendDecl appends one exported declaration block, or none.
func appendDecl(blocks []string, fset *token.FileSet, decl ast.Decl) []string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if line := funcLine(fset, d); line != "" {
			blocks = append(blocks, line)
		}
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			blocks = appendSpec(blocks, fset, d.Tok, spec)
		}
	}
	return blocks
}

// appendSpec renders exported consts and vars (with or without values)
// and types (with struct fields, the wire surface).
func appendSpec(blocks []string, fset *token.FileSet, tok token.Token, spec ast.Spec) []string {
	switch s := spec.(type) {
	case *ast.ValueSpec:
		switch tok {
		case token.CONST:
			for i, name := range s.Names {
				if !name.IsExported() {
					continue
				}
				if i < len(s.Values) {
					blocks = append(blocks, "const "+name.Name+" = "+expr(fset, s.Values[i]))
				} else {
					blocks = append(blocks, "const "+name.Name)
				}
			}
		case token.VAR:
			for _, name := range s.Names {
				if !name.IsExported() {
					continue
				}
				if s.Type != nil {
					blocks = append(blocks, "var "+name.Name+" "+expr(fset, s.Type))
				} else {
					blocks = append(blocks, "var "+name.Name)
				}
			}
		}
	case *ast.TypeSpec:
		if !s.Name.IsExported() {
			break
		}
		blocks = append(blocks, typeBlock(fset, s))
	}
	return blocks
}

// typeBlock renders a type declaration; structs list exported fields with
// tags because fields are the wire contract.
func typeBlock(fset *token.FileSet, s *ast.TypeSpec) string {
	name := "type " + s.Name.Name
	if s.TypeParams != nil {
		name += "[" + fieldList(fset, s.TypeParams) + "]"
	}
	if s.Assign.IsValid() {
		return name + " = " + expr(fset, s.Type)
	}
	st, ok := s.Type.(*ast.StructType)
	if !ok {
		return name + " " + expr(fset, s.Type)
	}
	var b strings.Builder
	b.WriteString(name + " struct {\n")
	for _, f := range st.Fields.List {
		if len(f.Names) == 0 {
			b.WriteString("  " + expr(fset, f.Type) + tag(f) + "\n")
			continue
		}
		for _, n := range f.Names {
			if n.IsExported() {
				b.WriteString("  " + n.Name + " " + expr(fset, f.Type) + tag(f) + "\n")
			}
		}
	}
	b.WriteString("}")
	return b.String()
}

// tag renders a struct field tag, or the empty string.
func tag(f *ast.Field) string {
	if f.Tag != nil {
		return " " + f.Tag.Value
	}
	return ""
}

// funcLine renders an exported func or method signature. The receiver
// renders with its name; unexported receiver types stay in the surface.
func funcLine(fset *token.FileSet, d *ast.FuncDecl) string {
	if !d.Name.IsExported() {
		return ""
	}
	var b strings.Builder
	b.Grow(128)
	b.WriteString("func ")
	if d.Recv != nil {
		b.WriteString("(")
		b.WriteString(fieldList(fset, d.Recv))
		b.WriteString(") ")
	}
	b.WriteString(d.Name.Name)
	if d.Type.TypeParams != nil {
		b.WriteString("[")
		b.WriteString(fieldList(fset, d.Type.TypeParams))
		b.WriteString("]")
	}
	b.WriteString("(")
	b.WriteString(fieldList(fset, d.Type.Params))
	b.WriteString(")")
	if d.Type.Results != nil {
		b.WriteString(" (")
		b.WriteString(fieldList(fset, d.Type.Results))
		b.WriteString(")")
	}
	return b.String()
}

// fieldList renders a parameter/result/receiver list.
func fieldList(fset *token.FileSet, fl *ast.FieldList) string {
	var parts []string
	for _, f := range fl.List {
		t := expr(fset, f.Type)
		if len(f.Names) == 0 {
			parts = append(parts, t)
			continue
		}
		var names []string
		for _, n := range f.Names {
			names = append(names, n.Name)
		}
		parts = append(parts, strings.Join(names, ", ")+" "+t)
	}
	return strings.Join(parts, ", ")
}

// expr renders an AST expression to source text.
func expr(fset *token.FileSet, e ast.Expr) string {
	var b strings.Builder
	if err := printer.Fprint(&b, fset, e); err != nil {
		return "<unprintable>"
	}
	return b.String()
}
