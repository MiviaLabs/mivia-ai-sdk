package workspace

import "os"

// Stat reports file info for path, relative to the Workspace's root.
func (w *Workspace) Stat(path string) (os.FileInfo, error) {
	resolved, err := w.resolve(path)
	if err != nil {
		return nil, err
	}
	return os.Stat(resolved)
}
