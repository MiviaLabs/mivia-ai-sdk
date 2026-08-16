// Command api_surface prints the exported API surface of every top-level
// package directory in this module. Output is deterministic; scripts/
// check_api.py diffs it against the locks in api/. Run `make api-update`
// to accept a deliberate surface change.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
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
func surface(dir string) ([]string, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, noTests, 0)
	if err != nil {
		return nil, err
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

func noTests(f os.FileInfo) bool { return !strings.HasSuffix(f.Name(), "_test.go") }

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

// appendSpec renders exported consts (with values) and types (with struct
// fields, the wire surface).
func appendSpec(blocks []string, fset *token.FileSet, tok token.Token, spec ast.Spec) []string {
	switch s := spec.(type) {
	case *ast.ValueSpec:
		if tok != token.CONST {
			break
		}
		for i, name := range s.Names {
			if name.IsExported() && i < len(s.Values) {
				blocks = append(blocks, "const "+name.Name+" = "+expr(fset, s.Values[i]))
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
	st, ok := s.Type.(*ast.StructType)
	if !ok {
		return "type " + s.Name.Name + " " + expr(fset, s.Type)
	}
	var b strings.Builder
	b.WriteString("type " + s.Name.Name + " struct {\n")
	for _, f := range st.Fields.List {
		for _, name := range f.Names {
			if name.IsExported() {
				tag := ""
				if f.Tag != nil {
					tag = " " + f.Tag.Value
				}
				b.WriteString("  " + name.Name + " " + expr(fset, f.Type) + tag + "\n")
			}
		}
	}
	b.WriteString("}")
	return b.String()
}

// funcLine renders an exported func or method signature.
func funcLine(fset *token.FileSet, d *ast.FuncDecl) string {
	if !d.Name.IsExported() {
		return ""
	}
	recv := ""
	if d.Recv != nil {
		rt := d.Recv.List[0].Type
		if star, ok := rt.(*ast.StarExpr); ok {
			rt = star.X
		}
		id, ok := rt.(*ast.Ident)
		if !ok || !id.IsExported() {
			return ""
		}
		recv = "(" + fieldList(fset, d.Recv) + ") "
	}
	sig := "func " + recv + d.Name.Name + "(" + fieldList(fset, d.Type.Params) + ")"
	if d.Type.Results != nil {
		sig += " (" + fieldList(fset, d.Type.Results) + ")"
	}
	return sig
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
