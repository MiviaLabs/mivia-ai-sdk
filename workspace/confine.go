package workspace

import (
	"fmt"
	"os"
	"path/filepath"
)

// resolve confines path to the Workspace's root: it joins and cleans
// path onto root, rejects lexical traversal, resolves any symlink
// components against the deepest existing ancestor, and rejects the
// result if it still escapes root. Every one of ReadFile, WriteFile,
// List, and Stat calls resolve before touching the filesystem.
func (w *Workspace) resolve(path string) (string, error) {
	var joined string
	if filepath.IsAbs(path) {
		joined = filepath.Clean(path)
	} else {
		joined = filepath.Clean(filepath.Join(w.root, path))
	}
	if !withinRoot(w.root, joined) {
		return "", fmt.Errorf("%w: %s", ErrEscape, path)
	}

	resolved, err := resolveDeepestExisting(joined)
	if err != nil {
		return "", fmt.Errorf("workspace: %w", err)
	}
	if !withinRoot(w.root, resolved) {
		return "", fmt.Errorf("%w: %s", ErrEscape, path)
	}
	return resolved, nil
}

// withinRoot reports whether p is root or lies under root.
func withinRoot(root, p string) bool {
	if p == root {
		return true
	}
	return len(p) > len(root) && p[:len(root)] == root && p[len(root)] == filepath.Separator
}

// resolveDeepestExisting resolves every symlink in p, including one
// at p's final component. When p does not fully exist, it resolves
// symlinks in the deepest existing ancestor and rejoins the missing
// suffix unresolved, matching the WriteFile case of a not-yet-created
// file.
func resolveDeepestExisting(p string) (string, error) {
	cur := p
	var suffix []string
	for {
		_, lstatErr := os.Lstat(cur)
		if lstatErr == nil {
			break
		}
		if !os.IsNotExist(lstatErr) {
			return "", lstatErr
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", lstatErr
		}
		suffix = append([]string{filepath.Base(cur)}, suffix...)
		cur = parent
	}

	resolved, err := filepath.EvalSymlinks(cur)
	if err != nil {
		return "", err
	}
	for _, s := range suffix {
		resolved = filepath.Join(resolved, s)
	}
	return resolved, nil
}
