package workspace

import "io"

// ReadFile reads the file at path, relative to the Workspace's root,
// through the open root. ReadFile has no size bound; a caller that
// needs one applies its own cap after the read. A read of a directory
// returns the raw filesystem error.
func (w *Workspace) ReadFile(path string) ([]byte, error) {
	resolved, err := w.resolve(path)
	if err != nil {
		return nil, err
	}
	f, err := w.r.Open(resolved)
	if err != nil {
		return nil, classify(path, err)
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}
