package workspace_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/workspace"
)

func TestTraversalEscape(t *testing.T) {
	dir := t.TempDir()
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	// The last two rows share one path string and differ only in the
	// disk state. Both must report ErrEscape: resolve cleans the path
	// before any syscall, so the verdict does not depend on the disk.
	vectors := []struct {
		name  string
		path  string
		setup func()
	}{
		{name: "parent", path: "../secret"},
		{name: "traversal through a missing component", path: "a/../../secret"},
		{name: "absolute", path: "/etc/secret-outside-root"},
		{
			name: "traversal through a real directory",
			path: "a/../../secret",
			setup: func() {
				if err := os.Mkdir(filepath.Join(dir, "a"), 0o700); err != nil {
					t.Fatalf("Mkdir(a): %v", err)
				}
			},
		},
	}
	for _, tc := range vectors {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup()
			}
			v := tc.path
			if _, err := w.ReadFile(v); !errors.Is(err, workspace.ErrEscape) {
				t.Errorf("ReadFile(%q) error = %v, want ErrEscape", v, err)
			}
			if err := w.WriteFile(v, []byte("x")); !errors.Is(err, workspace.ErrEscape) {
				t.Errorf("WriteFile(%q) error = %v, want ErrEscape", v, err)
			}
			if _, err := w.List(v); !errors.Is(err, workspace.ErrEscape) {
				t.Errorf("List(%q) error = %v, want ErrEscape", v, err)
			}
			if _, err := w.Stat(v); !errors.Is(err, workspace.ErrEscape) {
				t.Errorf("Stat(%q) error = %v, want ErrEscape", v, err)
			}
		})
	}
}

// assertSyscallEscape checks that err carries both ErrEscape and the
// *fs.PathError classify wraps through its two-verb form.
func assertSyscallEscape(t *testing.T, label string, err error) {
	t.Helper()
	if !errors.Is(err, workspace.ErrEscape) {
		t.Errorf("%s error = %v, want ErrEscape", label, err)
	}
	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) {
		t.Errorf("%s error = %v, want a *fs.PathError in the chain", label, err)
	}
}

// siblingFixture builds <tmp>/root and a sibling <tmp>/root-evil
// holding secret.txt, and returns both directories.
func siblingFixture(t *testing.T) (root, evil string) {
	t.Helper()
	base := t.TempDir()
	root = filepath.Join(base, "root")
	evil = filepath.Join(base, "root-evil")
	for _, dir := range []string{root, evil} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatalf("Mkdir(%q): %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(evil, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return root, evil
}

// TestSiblingPrefixEscape checks the separator term in withinRoot: a
// sibling directory whose name starts with the root's name escapes the
// root, even though its path carries the root as a string prefix.
func TestSiblingPrefixEscape(t *testing.T) {
	root, evil := siblingFixture(t)
	w, err := workspace.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	const secret = "../root-evil/secret.txt"
	got, err := w.ReadFile(secret)
	if !errors.Is(err, workspace.ErrEscape) {
		t.Errorf("ReadFile(%q) error = %v, want ErrEscape", secret, err)
	}
	if len(got) != 0 {
		t.Errorf("ReadFile(%q) = %q, want no bytes", secret, got)
	}

	const planted = "../root-evil/planted.txt"
	if err := w.WriteFile(planted, []byte("x")); !errors.Is(err, workspace.ErrEscape) {
		t.Errorf("WriteFile(%q) error = %v, want ErrEscape", planted, err)
	}
	if _, err := os.Stat(filepath.Join(evil, "planted.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Stat(planted file) error = %v, want ErrNotExist: WriteFile planted a file in the sibling", err)
	}

	const dir = "../root-evil"
	if _, err := w.List(dir); !errors.Is(err, workspace.ErrEscape) {
		t.Errorf("List(%q) error = %v, want ErrEscape", dir, err)
	}
	if _, err := w.Stat(secret); !errors.Is(err, workspace.ErrEscape) {
		t.Errorf("Stat(%q) error = %v, want ErrEscape", secret, err)
	}
}

func TestSymlinkEscapeFinalComponent(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	linkPath := filepath.Join(dir, "link")
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	_, readErr := w.ReadFile("link")
	assertSyscallEscape(t, "ReadFile(link)", readErr)
	_, listErr := w.List("link")
	assertSyscallEscape(t, "List(link)", listErr)
	_, statErr := w.Stat("link")
	assertSyscallEscape(t, "Stat(link)", statErr)
	assertSyscallEscape(t, "WriteFile(link)", w.WriteFile("link", []byte("x")))
}

func TestSymlinkEscapeIntermediateComponent(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "sub"), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	outsideFile := filepath.Join(outside, "sub", "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	linkPath := filepath.Join(dir, "link")
	if err := os.Symlink(outside, linkPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	_, readErr := w.ReadFile("link/sub/secret.txt")
	assertSyscallEscape(t, "ReadFile(link/sub/secret.txt)", readErr)
	_, listErr := w.List("link/sub")
	assertSyscallEscape(t, "List(link/sub)", listErr)
	_, statErr := w.Stat("link/sub/secret.txt")
	assertSyscallEscape(t, "Stat(link/sub/secret.txt)", statErr)

	// The write is refused by the directory creation, so this case is
	// the only pin on the MkdirAll branch of classify.
	writeErr := w.WriteFile("link/sub/planted.txt", []byte("x"))
	assertSyscallEscape(t, "WriteFile(link/sub/planted.txt)", writeErr)
	if _, err := os.Stat(filepath.Join(outside, "sub", "planted.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Stat(planted file) error = %v, want ErrNotExist: WriteFile planted a file outside the root", err)
	}
}

func TestWriteFileRejectsEscapingParent(t *testing.T) {
	dir := t.TempDir()
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	err = w.WriteFile("../outside/file.txt", []byte("x"))
	if !errors.Is(err, workspace.ErrEscape) {
		t.Errorf("WriteFile(escaping parent) error = %v, want ErrEscape", err)
	}
}

// TestClassifyPassesThrough pins the other direction of classify: an
// error that is not a confinement refusal keeps its own class. An
// implementation that tests only errors.As on *fs.PathError turns
// every missing-file and permission error into ErrEscape.
func TestClassifyPassesThrough(t *testing.T) {
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o700); err != nil {
		t.Fatalf("Mkdir(locked): %v", err)
	}
	if err := os.WriteFile(filepath.Join(locked, "f.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	if err := os.Symlink("nowhere", filepath.Join(dir, "dangling")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	cases := []struct {
		name     string
		path     string
		want     error
		skipRoot bool
	}{
		{name: "missing file", path: "missing.txt", want: fs.ErrNotExist},
		{name: "unreadable directory", path: "locked/f.txt", want: fs.ErrPermission, skipRoot: true},
		{name: "dangling symlink", path: "dangling", want: fs.ErrNotExist},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skipRoot && os.Geteuid() == 0 {
				t.Skip("the directory mode does not apply to the superuser")
			}
			_, err := w.ReadFile(tc.path)
			if !errors.Is(err, tc.want) {
				t.Errorf("ReadFile(%q) error = %v, want %v", tc.path, err, tc.want)
			}
			if errors.Is(err, workspace.ErrEscape) {
				t.Errorf("ReadFile(%q) error = %v, want no ErrEscape", tc.path, err)
			}
		})
	}
}

// TestAbsoluteSymlinkInsideRoot pins a deliberate tightening: os.Root
// refuses an absolute symlink, even when its target lies inside the
// root. A caller that needs the link followed uses a relative symlink.
func TestAbsoluteSymlinkInsideRoot(t *testing.T) {
	dir := t.TempDir()
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	if err := w.WriteFile("inside.txt", []byte("inside")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Symlink(filepath.Join(w.Root(), "inside.txt"), filepath.Join(w.Root(), "abslink")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	got, err := w.ReadFile("abslink")
	if !errors.Is(err, workspace.ErrEscape) {
		t.Errorf("ReadFile(abslink) error = %v, want ErrEscape", err)
	}
	if len(got) != 0 {
		t.Errorf("ReadFile(abslink) = %q, want no bytes", got)
	}
}
