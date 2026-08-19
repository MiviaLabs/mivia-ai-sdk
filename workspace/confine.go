package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// rootEscapeText is the inner error text os.Root reports when a path
// leaves the open root. The standard library exports no sentinel for
// it, so this constant gives the text compare one named point. See
// classify.
const rootEscapeText = "path escapes from parent"

// resolve turns path into a cleaned, root-relative name for an os.Root
// call. It is pure string work and touches no file, so it opens no
// window between a check and a syscall. An empty path resolves to ".".
// Every one of ReadFile, WriteFile, List, and Stat calls resolve
// first; os.Root then owns every symlink decision.
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
	rel, err := filepath.Rel(w.root, joined)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrEscape, path)
	}
	return rel, nil
}

// withinRoot reports whether p is root or lies under root. It goes
// through filepath.Rel, so a root of "/", which already ends in a
// separator, holds its children like any other root. p is the
// lexically cleaned path; a Rel failure denies.
func withinRoot(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// classify maps an os.Root confinement refusal onto ErrEscape, so a
// caller tests one sentinel for a lexical escape and a syscall escape
// alike. It returns any other error unchanged, including a missing
// file and a permission refusal.
func classify(path string, err error) error {
	if err == nil {
		return nil
	}
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) && pathErr.Err != nil && pathErr.Err.Error() == rootEscapeText {
		return fmt.Errorf("%w: %s: %w", ErrEscape, path, err)
	}
	return err
}
