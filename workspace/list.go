package workspace

import (
	"os"
	"slices"
	"strings"
)

// List reads the directory at path, relative to the Workspace's root,
// through the open root. It returns the entries sorted by filename,
// matching os.ReadDir; (*os.File).ReadDir returns raw directory order.
func (w *Workspace) List(path string) ([]os.DirEntry, error) {
	resolved, err := w.resolve(path)
	if err != nil {
		return nil, err
	}
	if err := w.denied(resolved, path); err != nil {
		return nil, err
	}
	f, err := w.r.Open(resolved)
	if err != nil {
		return nil, classify(path, err)
	}
	defer func() { _ = f.Close() }()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(entries, func(a, b os.DirEntry) int {
		return strings.Compare(a.Name(), b.Name())
	})
	return entries, nil
}
