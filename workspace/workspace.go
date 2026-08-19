package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrEscape reports that a path resolves outside a Workspace's root,
// through traversal or a symlink.
var ErrEscape = errors.New("workspace: path escapes root")

// Workspace confines filesystem access to one resolved root
// directory. It holds an open os.Root, so a Workspace owns a file
// descriptor and needs Close.
type Workspace struct {
	root string
	r    *os.Root
}

// Open resolves root to an absolute, symlink-free real path, opens it
// with os.OpenRoot, and returns a Workspace bound to the open root.
// root must exist and be a directory. Close the result.
func Open(root string) (*Workspace, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("workspace: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("workspace: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("workspace: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace: %s is not a directory", resolved)
	}
	r, err := os.OpenRoot(resolved)
	if err != nil {
		return nil, fmt.Errorf("workspace: %w", err)
	}
	return &Workspace{root: resolved, r: r}, nil
}

// Root returns the Workspace's resolved absolute root path.
func (w *Workspace) Root() string {
	return w.root
}

// Close closes the Workspace's open root. Close is idempotent,
// matching os.Root.Close. Every method returns an error matching
// fs.ErrClosed after Close. Close on a nil or zero-value Workspace
// returns nil, so a deferred Close before an error check is safe.
func (w *Workspace) Close() error {
	if w == nil || w.r == nil {
		return nil
	}
	return w.r.Close()
}
