package workspace_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/workspace"
)

func TestOpen(t *testing.T) {
	t.Run("missing root", func(t *testing.T) {
		_, err := workspace.Open(filepath.Join(t.TempDir(), "missing"))
		if err == nil {
			t.Fatal("Open(missing) = nil error, want error")
		}
	})

	t.Run("file instead of directory", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "f")
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		_, err := workspace.Open(file)
		if err == nil {
			t.Fatal("Open(file) = nil error, want error")
		}
	})

	t.Run("valid directory", func(t *testing.T) {
		dir := t.TempDir()
		w, err := workspace.Open(dir)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() { _ = w.Close() })
		resolvedDir, err := filepath.EvalSymlinks(dir)
		if err != nil {
			t.Fatalf("EvalSymlinks: %v", err)
		}
		if w.Root() != resolvedDir {
			t.Errorf("Root() = %q, want %q", w.Root(), resolvedDir)
		}
	})
}

func TestHappyPathRoundTrip(t *testing.T) {
	dir := t.TempDir()
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	data := []byte("hello workspace")
	if err := w.WriteFile("a/b/c.txt", data); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := w.ReadFile("a/b/c.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("ReadFile = %q, want %q", got, data)
	}

	// An absolute in-root path pins resolve's root-relative
	// conversion: os.Root refuses an absolute name.
	absPath := filepath.Join(w.Root(), "a/b/c.txt")
	gotAbs, err := w.ReadFile(absPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", absPath, err)
	}
	if string(gotAbs) != string(data) {
		t.Errorf("ReadFile(%q) = %q, want %q", absPath, gotAbs, data)
	}

	if err := w.WriteFile("a/b/d.txt", []byte("more")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	entries, err := w.List("a/b")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("List() = %d entries, want 2", len(entries))
	}

	info, err := w.Stat("a/b/c.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() != int64(len(data)) {
		t.Errorf("Stat().Size() = %d, want %d", info.Size(), len(data))
	}
}

// TestOpenRejectsBlankRoot pins that Open routes through
// Options.Validate. A blank root once bound the working directory,
// because filepath.Abs("") returns it.
func TestOpenRejectsBlankRoot(t *testing.T) {
	_, openErr := workspace.Open("")
	if openErr == nil {
		t.Fatal(`Open("") = nil error, want error`)
	}
	_, optErr := workspace.OpenWith(workspace.Options{})
	if optErr == nil {
		t.Fatal("OpenWith(Options{}) = nil error, want error")
	}
	if openErr.Error() != optErr.Error() {
		t.Errorf("Open(\"\") = %v, OpenWith(Options{}) = %v, want the same error", openErr, optErr)
	}
	if err := (workspace.Options{}).Validate(); err == nil || err.Error() != openErr.Error() {
		t.Errorf("Options{}.Validate() = %v, want Open(\"\")'s error %v", err, openErr)
	}
}

// TestOpenWithRejectsWhitespaceOnlyRoot pins the deliberate blank-root
// rejection for a single-space Root, distinct from the accidental,
// environment-dependent EvalSymlinks failure the same Root produces
// today. Fails against today's code, where Validate returns nil for a
// single-space Root and the real rejection, if any, comes later from
// filepath.EvalSymlinks instead.
func TestOpenWithRejectsWhitespaceOnlyRoot(t *testing.T) {
	err := (workspace.Options{Root: " "}).Validate()
	if err == nil {
		t.Fatal("Validate() = nil error, want the blank-root rejection")
	}
	if err.Error() != "workspace: Root is blank" {
		t.Fatalf("Validate() = %q, want %q", err.Error(), "workspace: Root is blank")
	}
}

// TestOpenWithAcceptsRealRoot is a second positive control: a real
// temporary directory still opens successfully, proving the trim does
// not reject a normal path.
func TestOpenWithAcceptsRealRoot(t *testing.T) {
	dir := t.TempDir()
	w, err := workspace.OpenWith(workspace.Options{Root: dir})
	if err != nil {
		t.Fatalf("OpenWith: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
}

// TestWriteFileMode pins the modes WriteFile applies at create: 0o600
// for the file and 0o700 for each created parent directory. No
// os.FileMode reaches the package from a caller.
func TestWriteFileMode(t *testing.T) {
	dir := t.TempDir()
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	if err := w.WriteFile("a/b/c.txt", []byte("x")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cases := []struct {
		path string
		want fs.FileMode
	}{
		{path: "a", want: 0o700},
		{path: "a/b", want: 0o700},
		{path: "a/b/c.txt", want: 0o600},
	}
	for _, tc := range cases {
		info, err := w.Stat(tc.path)
		if err != nil {
			t.Fatalf("Stat(%q): %v", tc.path, err)
		}
		if got := info.Mode().Perm(); got != tc.want {
			t.Errorf("Stat(%q).Mode().Perm() = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestWriteFileKeepsExistingMode pins that the create mode applies at
// create only: WriteFile never tightens an existing file.
func TestWriteFileKeepsExistingMode(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wide.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	if err := w.WriteFile("wide.txt", []byte("new")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	info, err := w.Stat("wide.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("Stat(wide.txt).Mode().Perm() = %v, want 0644", got)
	}
	data, err := w.ReadFile("wide.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "new" {
		t.Errorf("ReadFile = %q, want %q", data, "new")
	}
}

func TestListRootItself(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	entries, err := w.List(".")
	if err != nil {
		t.Fatalf("List(root): %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "f.txt" {
		t.Errorf("List(root) = %v, want one entry named f.txt", entries)
	}

	info, err := w.Stat(".")
	if err != nil {
		t.Fatalf("Stat(root): %v", err)
	}
	if !info.IsDir() {
		t.Error("Stat(root).IsDir() = false, want true")
	}
}

// TestListSortsByName pins List's os.ReadDir contract: the entries come
// back sorted by filename. (*os.File).ReadDir returns raw directory
// order, so a directory with one entry cannot catch a lost sort. The
// fixture writes the names out of order on purpose.
func TestListSortsByName(t *testing.T) {
	dir := t.TempDir()
	want := []string{"aaa", "bbb", "ccc", "ddd", "kkk", "mmm", "yyy", "zzz"}
	for _, name := range []string{"ddd", "ccc", "yyy", "bbb", "kkk", "aaa", "mmm", "zzz"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile(%q): %v", name, err)
		}
	}
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	entries, err := w.List(".")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := make([]string, 0, len(entries))
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if !slices.Equal(got, want) {
		t.Errorf("List(root) names = %v, want %v", got, want)
	}
}

func TestClose(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := w.ReadFile("f.txt"); !errors.Is(err, fs.ErrClosed) {
		t.Errorf("ReadFile after Close error = %v, want ErrClosed", err)
	}
	if err := w.WriteFile("g.txt", []byte("x")); !errors.Is(err, fs.ErrClosed) {
		t.Errorf("WriteFile after Close error = %v, want ErrClosed", err)
	}
	if _, err := w.List("."); !errors.Is(err, fs.ErrClosed) {
		t.Errorf("List after Close error = %v, want ErrClosed", err)
	}
	if _, err := w.Stat("f.txt"); !errors.Is(err, fs.ErrClosed) {
		t.Errorf("Stat after Close error = %v, want ErrClosed", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("second Close = %v, want nil", err)
	}

	// A deferred Close placed before the error check runs on a
	// zero-value or nil handle, so neither may panic.
	var zero workspace.Workspace
	if err := zero.Close(); err != nil {
		t.Errorf("zero-value Close = %v, want nil", err)
	}
	var nilW *workspace.Workspace
	if err := nilW.Close(); err != nil {
		t.Errorf("nil Close = %v, want nil", err)
	}
}

// TestZeroValueDeniesEveryMethod pins that a Workspace holding no root
// denies every method with ErrEscape. The package bans a panic, and a
// zero value reaches resolve before any root call, so resolve is where
// the absent root must be caught.
func TestZeroValueDeniesEveryMethod(t *testing.T) {
	cases := []struct {
		name string
		call func(w *workspace.Workspace) error
	}{
		{"ReadFile", func(w *workspace.Workspace) error { _, err := w.ReadFile("f.txt"); return err }},
		// A positive limit skips effectiveLimit's own guard, so this
		// case is the one that proves resolve catches the absent root.
		{"ReadFileLimit", func(w *workspace.Workspace) error { _, err := w.ReadFileLimit("f.txt", 10); return err }},
		{"WriteFile", func(w *workspace.Workspace) error { return w.WriteFile("f.txt", []byte("x")) }},
		{"List", func(w *workspace.Workspace) error { _, err := w.List("."); return err }},
		{"Stat", func(w *workspace.Workspace) error { _, err := w.Stat("f.txt"); return err }},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var zero workspace.Workspace
			if err := c.call(&zero); !errors.Is(err, workspace.ErrEscape) {
				t.Errorf("zero-value %s error = %v, want ErrEscape", c.name, err)
			}
			var nilW *workspace.Workspace
			if err := c.call(nilW); !errors.Is(err, workspace.ErrEscape) {
				t.Errorf("nil %s error = %v, want ErrEscape", c.name, err)
			}
		})
	}
}

// TestEmptyPath pins that resolve maps an empty path to ".", which
// os.Root accepts where it rejects an empty name, and that a read of a
// directory surfaces the raw filesystem error.
func TestEmptyPath(t *testing.T) {
	dir := t.TempDir()
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	for _, p := range []string{"", "."} {
		info, err := w.Stat(p)
		if err != nil {
			t.Fatalf("Stat(%q): %v", p, err)
		}
		if !info.IsDir() {
			t.Errorf("Stat(%q).IsDir() = false, want true", p)
		}
	}

	for _, p := range []string{"", "."} {
		_, err := w.ReadFile(p)
		if err == nil {
			t.Fatalf("ReadFile(%q) = nil error, want a directory-read error", p)
		}
		if errors.Is(err, workspace.ErrEscape) {
			t.Errorf("ReadFile(%q) error = %v, want a raw filesystem error, not ErrEscape", p, err)
		}
		if errors.Is(err, fs.ErrNotExist) {
			t.Errorf("ReadFile(%q) error = %v, want a raw directory-read error, not ErrNotExist", p, err)
		}
	}
}

// TestListOnFile pins the read error of a listing: an open succeeds on
// a regular file, so the directory read is the stage that refuses.
func TestListOnFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	entries, err := w.List("f.txt")
	if err == nil {
		t.Fatal("List(f.txt) = nil error, want a directory-read error")
	}
	if errors.Is(err, workspace.ErrEscape) {
		t.Errorf("List(f.txt) error = %v, want a raw filesystem error, not ErrEscape", err)
	}
	if entries != nil {
		t.Errorf("List(f.txt) = %v, want no entries", entries)
	}
}
