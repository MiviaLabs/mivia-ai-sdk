package workspace

import "os"

// Stat reports file info for path, relative to the Workspace's root,
// through the open root.
func (w *Workspace) Stat(path string) (os.FileInfo, error) {
	resolved, err := w.resolve(path)
	if err != nil {
		return nil, err
	}
	info, err := w.r.Stat(resolved)
	if err != nil {
		return nil, classify(path, err)
	}
	return info, nil
}
