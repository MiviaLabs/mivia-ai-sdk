package workspace

import (
	"os"
	"path/filepath"
)

// WriteFile writes data to the file at path, relative to the
// Workspace's root, creating any missing parent directory under root.
func (w *Workspace) WriteFile(path string, data []byte, perm os.FileMode) error {
	resolved, err := w.resolve(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o700); err != nil {
		return err
	}
	return os.WriteFile(resolved, data, perm)
}
