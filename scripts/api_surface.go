// Command api_surface prints the exported API surface of every package
// directory in this module, at any depth. Output is deterministic;
// scripts/check_api.py diffs it against the locks in api/. Run `make
// api-update` to accept a deliberate surface change.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// scriptsDir is the gate-tooling directory, excluded from the surface.
const scriptsDir = "scripts"

// alwaysBuildTags names every build tag this module gates a file
// behind, today only ledger_sqlite (ledger/sqlite_store.go). The lock
// tool always includes a tag-gated file's exported surface, so
// api/*.txt captures the symbol regardless of tag; only the real
// `go build` and `go test` invocations honor the tag and keep the
// dependency out of the default binary. Extend this list when a
// future package adds its own gated file. Keep it equal to BUILD_TAGS
// in scripts/go_packages.py, whose constraint scan fails on a file
// behind any other constraint.
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
		fmt.Println("package " + dir)
		for _, line := range lines {
			fmt.Println("  " + line)
		}
	}
}

// goPackage is the subset of `go list -json` output this tool reads.
type goPackage struct {
	Dir  string
	Root string
}

// packageDirs lists every package directory of this module, relative to
// the module root and at any depth. It unions a default-tag `go list`
// run with a run over alwaysBuildTags, so a fully tag-gated package
// still reaches the lock. go list drops `_`- and `.`-prefixed segments
// and testdata; excluded drops the rest.
func packageDirs() ([]string, error) {
	seen := make(map[string]bool)
	for _, tags := range []string{"", strings.Join(alwaysBuildTags, ",")} {
		pkgs, err := goList(tags)
		if err != nil {
			return nil, err
		}
		for _, p := range pkgs {
			rel, err := filepath.Rel(p.Root, p.Dir)
			if err != nil {
				return nil, err
			}
			rel = filepath.ToSlash(rel)
			if excluded(rel) {
				continue
			}
			seen[rel] = true
		}
	}
	dirs := make([]string, 0, len(seen))
	for dir := range seen {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs, nil
}

// excluded reports whether the exclusion set drops a package path: the
// module root, the scripts tree, or an external test package.
func excluded(rel string) bool {
	if rel == "." || rel == scriptsDir || strings.HasPrefix(rel, scriptsDir+"/") {
		return true
	}
	return strings.HasSuffix(rel, "_test")
}

// goList runs `go list -json ./...` for one tag configuration. It
// returns an error carrying the go stderr when the run fails; a
// toolchain failure is never an empty package set.
func goList(tags string) ([]goPackage, error) {
	args := []string{"list", "-json"}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	args = append(args, "./...")
	cmd := exec.Command("go", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	dec := json.NewDecoder(&stdout)
	var pkgs []goPackage
	for {
		var p goPackage
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			return pkgs, nil
		} else if err != nil {
			return nil, err
		}
		pkgs = append(pkgs, p)
	}
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
	var b strings.Builder
	b.WriteString("type ")
	b.WriteString(s.Name.Name)
	if s.TypeParams != nil {
		b.WriteString("[")
		b.WriteString(fieldList(fset, s.TypeParams))
		b.WriteString("]")
	}
	if s.Assign.IsValid() {
		b.WriteString(" = ")
		b.WriteString(expr(fset, s.Type))
		return b.String()
	}
	st, ok := s.Type.(*ast.StructType)
	if !ok {
		b.WriteString(" ")
		b.WriteString(expr(fset, s.Type))
		return b.String()
	}
	b.WriteString(" struct {\n")
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
