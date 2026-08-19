package workspace

import (
	"path/filepath"
)

// writeFileMode is the mode WriteFile creates a new file with, and
// writeDirMode the mode it creates a missing parent directory with.
// Both are owner-only: no group bit, no other bit, no setuid or setgid
// bit. This package takes no os.FileMode from a caller, so no
// model-supplied mode reaches a syscall.
const (
	writeFileMode = 0o600
	writeDirMode  = 0o700
)

// WriteFile writes data to the file at path, relative to the
// Workspace's root, through the open root, creating any missing
// parent directory under the root. It creates a new file with mode
// 0o600 and a new directory with mode 0o700, and it leaves an
// existing file's or directory's mode alone. Both syscalls run
// through classify, because an escaping intermediate component is
// refused by the directory creation before the write runs.
func (w *Workspace) WriteFile(path string, data []byte) error {
	resolved, err := w.resolve(path)
	if err != nil {
		return err
	}
	if err := w.denied(resolved, path); err != nil {
		return err
	}
	if dir := filepath.Dir(resolved); dir != "." {
		if err := w.r.MkdirAll(dir, writeDirMode); err != nil {
			return classify(path, err)
		}
	}
	return classify(path, w.r.WriteFile(resolved, data, writeFileMode))
}
