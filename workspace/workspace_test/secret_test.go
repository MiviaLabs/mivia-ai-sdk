package workspace_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/secretpath"
	"github.com/MiviaLabs/mivia-ai-sdk/workspace"
)

// denyPatterns is the pattern list the name-policy cases share.
var denyPatterns = []string{".env", "secrets/"}

// newMatcher compiles patterns or fails the test.
func newMatcher(t *testing.T, patterns []string) *secretpath.Matcher {
	t.Helper()
	m, err := secretpath.NewMatcher(patterns)
	if err != nil {
		t.Fatalf("NewMatcher(%v): %v", patterns, err)
	}
	return m
}

// openDeny opens a Workspace on root under a Deny matcher compiled
// from patterns. A nil patterns slice leaves Deny nil.
func openDeny(t *testing.T, root string, patterns []string) *workspace.Workspace {
	t.Helper()
	opts := workspace.Options{Root: root}
	if patterns != nil {
		opts.Deny = newMatcher(t, patterns)
	}
	w, err := workspace.OpenWith(opts)
	if err != nil {
		t.Fatalf("OpenWith(%q): %v", root, err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

// writeUnderTest writes a fixture file outside the package API, so no
// deny rule can change the fixture.
func writeUnderTest(t *testing.T, root, name, data string) {
	t.Helper()
	full := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", full, err)
	}
}

// callAll runs every method that holds the deny check against path and
// reports one error per method name. The ReadFile row pins that
// ReadFile inherits the check from ReadFileLimit and adds none.
func callAll(w *workspace.Workspace, path string) map[string]error {
	_, readLimitErr := w.ReadFileLimit(path, 0)
	_, readErr := w.ReadFile(path)
	_, listErr := w.List(path)
	_, statErr := w.Stat(path)
	return map[string]error{
		"ReadFileLimit": readLimitErr,
		"ReadFile":      readErr,
		"WriteFile":     w.WriteFile(path, []byte("x")),
		"List":          listErr,
		"Stat":          statErr,
	}
}

func TestDenyAcrossMethods(t *testing.T) {
	root := t.TempDir()
	writeUnderTest(t, root, ".env", "K=V")
	writeUnderTest(t, root, "secrets/api.json", "{}")
	writeUnderTest(t, root, "notes.txt", "hello")
	writeUnderTest(t, root, "data/notes.txt", "hello")
	w := openDeny(t, root, denyPatterns)

	vectors := []struct {
		name   string
		path   string
		denied bool
	}{
		{name: "denied file", path: ".env", denied: true},
		{name: "denied file under a denied directory", path: "secrets/api.json", denied: true},
		{name: "denied directory itself", path: "secrets", denied: true},
		{name: "permitted file", path: "notes.txt"},
		{name: "permitted file in a permitted directory", path: "data/notes.txt"},
	}
	for _, tc := range vectors {
		t.Run(tc.name, func(t *testing.T) {
			for method, err := range callAll(w, tc.path) {
				got := errors.Is(err, workspace.ErrSecretPath)
				if got != tc.denied {
					t.Errorf("%s(%q) error = %v, ErrSecretPath = %t, want %t",
						method, tc.path, err, got, tc.denied)
				}
			}
		})
	}
}

// TestRootPathIsNotRefused pins the walk's "." branch. resolve maps
// both "." and "" to ".", which names the root itself and holds no
// component to walk. Without that branch a Deny-configured workspace
// loses its own root listing, which is subagent's primary list call.
func TestRootPathIsNotRefused(t *testing.T) {
	root := t.TempDir()
	writeUnderTest(t, root, "notes.txt", "ok")
	w := openDeny(t, root, denyPatterns)

	for _, p := range []string{".", ""} {
		if _, err := w.List(p); err != nil {
			t.Errorf("List(%q) = %v, want nil", p, err)
		}
		if _, err := w.Stat(p); err != nil {
			t.Errorf("Stat(%q) = %v, want nil", p, err)
		}
	}
}

func TestDeepPermittedPathIsNotRefused(t *testing.T) {
	root := t.TempDir()
	writeUnderTest(t, root, "a/b/c/d/e.txt", "deep")
	w := openDeny(t, root, denyPatterns)

	data, err := w.ReadFile("a/b/c/d/e.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "deep" {
		t.Errorf("ReadFile = %q, want %q", data, "deep")
	}
	if _, err := w.List("a/b/c"); err != nil {
		t.Errorf("List: %v", err)
	}
}

func TestDeniedWriteTouchesNothing(t *testing.T) {
	root := t.TempDir()
	writeUnderTest(t, root, "secrets/api.json", "original")
	w := openDeny(t, root, denyPatterns)

	if err := w.WriteFile("secrets/api.json", []byte("pwned")); !errors.Is(err, workspace.ErrSecretPath) {
		t.Fatalf("WriteFile error = %v, want ErrSecretPath", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "secrets", "api.json"))
	if err != nil {
		t.Fatalf("ReadFile fixture: %v", err)
	}
	if string(data) != "original" {
		t.Errorf("fixture = %q, want %q", data, "original")
	}
	// The check runs before os.Root.MkdirAll, so a denied write under a
	// missing parent creates no directory.
	if err := w.WriteFile("secrets/new/deep.json", []byte("x")); !errors.Is(err, workspace.ErrSecretPath) {
		t.Fatalf("WriteFile error = %v, want ErrSecretPath", err)
	}
	if _, err := os.Stat(filepath.Join(root, "secrets", "new")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Stat(secrets/new) error = %v, want fs.ErrNotExist", err)
	}
}

func TestEscapeBeatsDeny(t *testing.T) {
	root := t.TempDir()
	w := openDeny(t, root, denyPatterns)

	_, err := w.ReadFile("../.env")
	if !errors.Is(err, workspace.ErrEscape) {
		t.Errorf("ReadFile error = %v, want ErrEscape", err)
	}
	if errors.Is(err, workspace.ErrSecretPath) {
		t.Errorf("ReadFile error = %v, want no ErrSecretPath", err)
	}
}

func TestDeniedAndMissing(t *testing.T) {
	root := t.TempDir()
	w := openDeny(t, root, denyPatterns)

	_, err := w.ReadFile(".env")
	if !errors.Is(err, workspace.ErrSecretPath) {
		t.Errorf("ReadFile error = %v, want ErrSecretPath", err)
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ReadFile error = %v, want no fs.ErrNotExist", err)
	}
}

func TestDenyBeatsClosedWorkspace(t *testing.T) {
	root := t.TempDir()
	writeUnderTest(t, root, ".env", "K=V")
	writeUnderTest(t, root, "notes.txt", "hello")
	w := openDeny(t, root, denyPatterns)
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := w.ReadFile(".env"); !errors.Is(err, workspace.ErrSecretPath) {
		t.Errorf("ReadFile(.env) error = %v, want ErrSecretPath", err)
	}
	if _, err := w.ReadFile("notes.txt"); !errors.Is(err, fs.ErrClosed) {
		t.Errorf("ReadFile(notes.txt) error = %v, want fs.ErrClosed", err)
	}
}

func TestAbsoluteCallerPathDenied(t *testing.T) {
	root := t.TempDir()
	writeUnderTest(t, root, "secrets/api.json", "{}")
	w := openDeny(t, root, denyPatterns)

	// resolve returns "secrets/api.json", which the pattern matches. The
	// raw caller string carries the root's absolute prefix, which it
	// does not, so this vector separates the two candidate inputs.
	path := filepath.Join(w.Root(), "secrets", "api.json")
	if _, err := w.ReadFile(path); !errors.Is(err, workspace.ErrSecretPath) {
		t.Errorf("ReadFile(%q) error = %v, want ErrSecretPath", path, err)
	}
}

func TestNilDenyPermitsEverything(t *testing.T) {
	root := t.TempDir()
	writeUnderTest(t, root, ".env", "K=V")
	writeUnderTest(t, root, "secrets/api.json", "{}")
	w := openDeny(t, root, nil)

	for _, path := range []string{".env", "secrets/api.json"} {
		for method, err := range callAll(w, path) {
			if errors.Is(err, workspace.ErrSecretPath) {
				t.Errorf("%s(%q) error = %v, want no ErrSecretPath", method, path, err)
			}
		}
	}
}

// TestOptionsValidateDeny pins the Deny field's one rule: nil passes
// and a compiled matcher passes. read_limit_test.go's
// TestOptionsValidate owns the blank-root and limit rows.
func TestOptionsValidateDeny(t *testing.T) {
	root := t.TempDir()
	vectors := []struct {
		name    string
		opts    workspace.Options
		wantErr bool
	}{
		{name: "blank root", opts: workspace.Options{}, wantErr: true},
		{name: "root with a nil deny", opts: workspace.Options{Root: root}},
		{
			name: "root with a compiled deny",
			opts: workspace.Options{Root: root, Deny: newMatcher(t, denyPatterns)},
		},
	}
	for _, tc := range vectors {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.opts.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate error = %v, wantErr %t", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			w, err := workspace.OpenWith(tc.opts)
			if err != nil {
				t.Fatalf("OpenWith: %v", err)
			}
			t.Cleanup(func() { _ = w.Close() })
			if w == nil {
				t.Fatal("OpenWith returned a nil Workspace")
			}
		})
	}
}

// aliasFixture builds the aliasing attack: a denied secrets/key.pem, a
// file symlink innocent.txt pointing at it, and a directory symlink
// sdir pointing at secrets. Both links are relative, because os.Root
// refuses an absolute symlink.
func aliasFixture(t *testing.T) string {
	t.Helper()
	skipWithoutSymlinks(t)
	root := t.TempDir()
	writeUnderTest(t, root, "secrets/key.pem", "TOP-SECRET")
	link(t, filepath.Join("secrets", "key.pem"), filepath.Join(root, "innocent.txt"))
	link(t, "secrets", filepath.Join(root, "sdir"))
	return root
}

// link creates a symlink or fails the test.
func link(t *testing.T, target, name string) {
	t.Helper()
	if err := os.Symlink(target, name); err != nil {
		t.Fatalf("Symlink(%q, %q): %v", target, name, err)
	}
}

func TestFileSymlinkToDeniedFile(t *testing.T) {
	root := aliasFixture(t)
	w := openDeny(t, root, denyPatterns)

	data, err := w.ReadFile("innocent.txt")
	if !errors.Is(err, workspace.ErrSecretPath) {
		t.Errorf("ReadFile error = %v, want ErrSecretPath", err)
	}
	if data != nil {
		t.Errorf("ReadFile returned %q, want no bytes", data)
	}
}

func TestDirectorySymlinkToDeniedDirectory(t *testing.T) {
	root := aliasFixture(t)
	w := openDeny(t, root, denyPatterns)

	data, err := w.ReadFile("sdir/key.pem")
	if !errors.Is(err, workspace.ErrSecretPath) {
		t.Errorf("ReadFile error = %v, want ErrSecretPath", err)
	}
	if data != nil {
		t.Errorf("ReadFile returned %q, want no bytes", data)
	}
}

func TestWriteThroughSymlinkDenied(t *testing.T) {
	root := aliasFixture(t)
	w := openDeny(t, root, denyPatterns)

	if err := w.WriteFile("innocent.txt", []byte("PWNED")); !errors.Is(err, workspace.ErrSecretPath) {
		t.Fatalf("WriteFile error = %v, want ErrSecretPath", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "secrets", "key.pem"))
	if err != nil {
		t.Fatalf("ReadFile fixture: %v", err)
	}
	if string(data) != "TOP-SECRET" {
		t.Errorf("secrets/key.pem = %q, want %q", data, "TOP-SECRET")
	}
}

func TestSymlinkPermittedWithoutDeny(t *testing.T) {
	root := aliasFixture(t)
	w := openDeny(t, root, nil)

	data, err := w.ReadFile("innocent.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "TOP-SECRET" {
		t.Errorf("ReadFile = %q, want %q", data, "TOP-SECRET")
	}
}

func TestWalkErrorNamesSymlink(t *testing.T) {
	root := aliasFixture(t)
	writeUnderTest(t, root, ".env", "K=V")
	w := openDeny(t, root, denyPatterns)

	vectors := []struct {
		name     string
		path     string
		wantWalk bool
	}{
		{name: "walk refusal", path: "innocent.txt", wantWalk: true},
		{name: "name refusal", path: ".env"},
	}
	for _, tc := range vectors {
		t.Run(tc.name, func(t *testing.T) {
			_, err := w.ReadFile(tc.path)
			if !errors.Is(err, workspace.ErrSecretPath) {
				t.Fatalf("ReadFile(%q) error = %v, want ErrSecretPath", tc.path, err)
			}
			got := strings.Contains(err.Error(), "symlink component")
			if got != tc.wantWalk {
				t.Errorf("ReadFile(%q) error = %q, holds %q = %t, want %t",
					tc.path, err, "symlink component", got, tc.wantWalk)
			}
		})
	}
}

// outsideLinkFixture builds a root holding one relative symlink named
// name that points at a file outside the root. It returns the root.
func outsideLinkFixture(t *testing.T, name string) string {
	t.Helper()
	skipWithoutSymlinks(t)
	base := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("Mkdir(root): %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "outside.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatalf("WriteFile(outside.txt): %v", err)
	}
	link(t, filepath.Join("..", "outside.txt"), filepath.Join(root, name))
	return root
}

func TestDeniedNameLinkOutOfRoot(t *testing.T) {
	root := outsideLinkFixture(t, ".env")
	w := openDeny(t, root, denyPatterns)

	_, err := w.ReadFile(".env")
	if !errors.Is(err, workspace.ErrSecretPath) {
		t.Errorf("ReadFile error = %v, want ErrSecretPath", err)
	}
	if errors.Is(err, workspace.ErrEscape) {
		t.Errorf("ReadFile error = %v, want no ErrEscape", err)
	}
}

func TestPermittedNameLinkOutOfRoot(t *testing.T) {
	root := outsideLinkFixture(t, "notes.txt")
	vectors := []struct {
		name     string
		patterns []string
		want     error
		unwanted error
	}{
		{name: "deny set", patterns: denyPatterns, want: workspace.ErrSecretPath, unwanted: workspace.ErrEscape},
		{name: "nil deny", want: workspace.ErrEscape, unwanted: workspace.ErrSecretPath},
	}
	for _, tc := range vectors {
		t.Run(tc.name, func(t *testing.T) {
			w := openDeny(t, root, tc.patterns)
			_, err := w.ReadFile("notes.txt")
			if !errors.Is(err, tc.want) {
				t.Errorf("ReadFile error = %v, want %v", err, tc.want)
			}
			if errors.Is(err, tc.unwanted) {
				t.Errorf("ReadFile error = %v, want no %v", err, tc.unwanted)
			}
		})
	}
}

func TestDanglingSymlinkDenied(t *testing.T) {
	skipWithoutSymlinks(t)
	root := t.TempDir()
	link(t, "missing.txt", filepath.Join(root, "dangle"))

	vectors := []struct {
		name     string
		patterns []string
		want     error
		unwanted error
	}{
		{name: "deny set", patterns: denyPatterns, want: workspace.ErrSecretPath, unwanted: fs.ErrNotExist},
		{name: "nil deny", want: fs.ErrNotExist, unwanted: workspace.ErrSecretPath},
	}
	for _, tc := range vectors {
		t.Run(tc.name, func(t *testing.T) {
			w := openDeny(t, root, tc.patterns)
			_, err := w.ReadFile("dangle")
			if !errors.Is(err, tc.want) {
				t.Errorf("ReadFile error = %v, want %v", err, tc.want)
			}
			if errors.Is(err, tc.unwanted) {
				t.Errorf("ReadFile error = %v, want no %v", err, tc.unwanted)
			}
		})
	}
}
