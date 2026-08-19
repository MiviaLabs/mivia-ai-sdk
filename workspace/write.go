package workspace

import (
	"os"
	"path/filepath"
)

// WriteFile writes data to the file at path, relative to the
// Workspace's root, through the open root, creating any missing
// parent directory under the root. Both syscalls run through
// classify, because an escaping intermediate component is refused by
// the directory creation before the write runs.
func (w *Workspace) WriteFile(path string, data []byte, perm os.FileMode) error {
	resolved, err := w.resolve(path)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(resolved); dir != "." {
		if err := w.r.MkdirAll(dir, 0o700); err != nil {
			return classify(path, err)
		}
	}
	return classify(path, w.r.WriteFile(resolved, data, perm))
}
