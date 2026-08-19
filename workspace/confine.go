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
// first; os.Root then owns every symlink decision. A nil or
// zero-value Workspace holds no root, so resolve denies it here
// rather than letting a method dereference the absent root.
func (w *Workspace) resolve(path string) (string, error) {
	if w == nil || w.r == nil {
		return "", fmt.Errorf("%w: %s", ErrEscape, path)
	}
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

// denied holds the whole secret-path rule, so each of ReadFileLimit,
// WriteFile, List, and Stat calls it in one line and states no rule of
// its own. resolved is resolve's cleaned root-relative name, which the
// matcher reads; path is the caller's raw string, used in the error
// text only. A nil deny matcher returns nil at once, so a Workspace
// with no policy makes no extra syscall. The name check runs first,
// because it is pure string work and is the common denial. The symlink
// walk runs second, and only when the name check permits. See
// ErrSecretPath.
func (w *Workspace) denied(resolved, path string) error {
	if w == nil || w.deny == nil {
		return nil
	}
	if w.deny.Matches(resolved) {
		return fmt.Errorf("%w: %s", ErrSecretPath, path)
	}
	if w.hasSymlinkComponent(resolved) {
		return fmt.Errorf("%w: %s: symlink component", ErrSecretPath, path)
	}
	return nil
}

// hasSymlinkComponent reports whether any prefix of resolved is a
// symlink. It walks the prefixes shortest first and includes the final
// component, because a permitted name can itself be the link to a
// denied file. A resolved value of "." walks nothing: the root is the
// open os.Root and holds no component to test. A failing Lstat is not
// a symlink, so the walk ignores the error and continues. That is
// safe, because statat and openat traverse the same chain from the
// same root descriptor, so any state that fails an Lstat fails the
// following open with the same error. Shortest-first order also keeps
// a later failure from masking an earlier link.
func (w *Workspace) hasSymlinkComponent(resolved string) bool {
	if resolved == "." {
		return false
	}
	parts := strings.Split(resolved, string(filepath.Separator))
	for i := range parts {
		prefix := filepath.Join(parts[:i+1]...)
		info, err := w.r.Lstat(prefix)
		if err != nil {
			continue
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return true
		}
	}
	return false
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
