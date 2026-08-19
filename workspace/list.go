package workspace

import "os"

// List reads the directory at path, relative to the Workspace's root.
func (w *Workspace) List(path string) ([]os.DirEntry, error) {
	resolved, err := w.resolve(path)
	if err != nil {
		return nil, err
	}
	return os.ReadDir(resolved)
}
