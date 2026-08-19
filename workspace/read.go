package workspace

import "os"

// ReadFile reads the file at path, relative to the Workspace's root.
// ReadFile has no size bound; a caller that needs one applies its own
// cap after the read.
func (w *Workspace) ReadFile(path string) ([]byte, error) {
	resolved, err := w.resolve(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(resolved)
}
