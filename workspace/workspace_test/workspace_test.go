package workspace_test

import (
	"errors"
	"os"
	"path/filepath"
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
		resolvedDir, err := filepath.EvalSymlinks(dir)
		if err != nil {
			t.Fatalf("EvalSymlinks: %v", err)
		}
		if w.Root() != resolvedDir {
			t.Errorf("Root() = %q, want %q", w.Root(), resolvedDir)
		}
	})
}

func TestTraversalEscape(t *testing.T) {
	dir := t.TempDir()
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	vectors := []string{"../secret", "a/../../secret", "/etc/secret-outside-root"}
	for _, v := range vectors {
		t.Run(v, func(t *testing.T) {
			if _, err := w.ReadFile(v); !errors.Is(err, workspace.ErrEscape) {
				t.Errorf("ReadFile(%q) error = %v, want ErrEscape", v, err)
			}
			if err := w.WriteFile(v, []byte("x"), 0o600); !errors.Is(err, workspace.ErrEscape) {
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

	const secret = "../root-evil/secret.txt"
	got, err := w.ReadFile(secret)
	if !errors.Is(err, workspace.ErrEscape) {
		t.Errorf("ReadFile(%q) error = %v, want ErrEscape", secret, err)
	}
	if len(got) != 0 {
		t.Errorf("ReadFile(%q) = %q, want no bytes", secret, got)
	}

	const planted = "../root-evil/planted.txt"
	if err := w.WriteFile(planted, []byte("x"), 0o600); !errors.Is(err, workspace.ErrEscape) {
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

	if _, err := w.ReadFile("link"); !errors.Is(err, workspace.ErrEscape) {
		t.Errorf("ReadFile(link) error = %v, want ErrEscape", err)
	}
	if _, err := w.List("link"); !errors.Is(err, workspace.ErrEscape) {
		t.Errorf("List(link) error = %v, want ErrEscape", err)
	}
	if _, err := w.Stat("link"); !errors.Is(err, workspace.ErrEscape) {
		t.Errorf("Stat(link) error = %v, want ErrEscape", err)
	}
	if err := w.WriteFile("link", []byte("x"), 0o600); !errors.Is(err, workspace.ErrEscape) {
		t.Errorf("WriteFile(link) error = %v, want ErrEscape", err)
	}
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

	if _, err := w.ReadFile("link/sub/secret.txt"); !errors.Is(err, workspace.ErrEscape) {
		t.Errorf("ReadFile(link/sub/secret.txt) error = %v, want ErrEscape", err)
	}
	if _, err := w.List("link/sub"); !errors.Is(err, workspace.ErrEscape) {
		t.Errorf("List(link/sub) error = %v, want ErrEscape", err)
	}
	if _, err := w.Stat("link/sub/secret.txt"); !errors.Is(err, workspace.ErrEscape) {
		t.Errorf("Stat(link/sub/secret.txt) error = %v, want ErrEscape", err)
	}
}

func TestHappyPathRoundTrip(t *testing.T) {
	dir := t.TempDir()
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	data := []byte("hello workspace")
	if err := w.WriteFile("a/b/c.txt", data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := w.ReadFile("a/b/c.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("ReadFile = %q, want %q", got, data)
	}

	if err := w.WriteFile("a/b/d.txt", []byte("more"), 0o600); err != nil {
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

func TestListRootItself(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

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

func TestWriteFileRejectsEscapingParent(t *testing.T) {
	dir := t.TempDir()
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	err = w.WriteFile("../outside/file.txt", []byte("x"), 0o600)
	if !errors.Is(err, workspace.ErrEscape) {
		t.Errorf("WriteFile(escaping parent) error = %v, want ErrEscape", err)
	}
}
