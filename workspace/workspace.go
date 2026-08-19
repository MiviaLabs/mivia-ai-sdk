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
// directory.
type Workspace struct {
	root string
}

// Open resolves root to an absolute, symlink-free real path and
// returns a Workspace bound to it. root must exist and be a
// directory.
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
	return &Workspace{root: resolved}, nil
}

// Root returns the Workspace's resolved absolute root path.
func (w *Workspace) Root() string {
	return w.root
}
