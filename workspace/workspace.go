package workspace

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

// DefaultMaxReadBytes is the read bound a Workspace uses when its
// caller sets no MaxReadBytes. The bound fails closed: an unset field
// yields a bounded workspace, not an unbounded one.
const DefaultMaxReadBytes int64 = 10 << 20

// Unbounded removes the read bound. Set it on Options.MaxReadBytes for
// a whole Workspace, or pass it to ReadFileLimit for one call.
const Unbounded int64 = -1

// maxReadLimit is the largest accepted read bound. It is
// math.MaxInt64-1, so the limit+1 read in ReadFileLimit cannot
// overflow.
const maxReadLimit int64 = math.MaxInt64 - 1

// ErrEscape reports that a path resolves outside a Workspace's root,
// through traversal or a symlink.
var ErrEscape = errors.New("workspace: path escapes root")

// ErrTooLarge reports that a file is longer than the read's effective
// bound. It wraps no filesystem error, because it is this package's
// own policy refusal.
var ErrTooLarge = errors.New("workspace: file exceeds read limit")

// ErrInvalidLimit reports a read bound that is neither Unbounded, nor
// zero, nor a positive value at or under maxReadLimit.
var ErrInvalidLimit = errors.New("workspace: invalid read limit")

// Workspace confines filesystem access to one resolved root
// directory. It holds an open os.Root, so a Workspace owns a file
// descriptor and needs Close.
type Workspace struct {
	root         string
	r            *os.Root
	maxReadBytes int64
}

// Options configures one Workspace at open time. See OpenWith.
type Options struct {
	// Root is the directory the Workspace confines access to.
	Root string
	// MaxReadBytes bounds one read. Zero selects DefaultMaxReadBytes
	// and Unbounded removes the bound. See Validate.
	MaxReadBytes int64
}

// Validate reports whether o names a usable Workspace. Root must not
// be blank. MaxReadBytes must be Unbounded, zero, or a positive value
// at or under maxReadLimit.
func (o Options) Validate() error {
	if o.Root == "" {
		return errors.New("workspace: Root is blank")
	}
	return validateLimit(o.MaxReadBytes)
}

// validateLimit enforces the one read-bound rule: Unbounded, zero, or
// a positive value at or under maxReadLimit passes; any other value
// returns ErrInvalidLimit. Options.Validate and effectiveLimit both
// call it, so the rule has one enforcer.
func validateLimit(v int64) error {
	if v == Unbounded || (v >= 0 && v <= maxReadLimit) {
		return nil
	}
	return fmt.Errorf("%w: %d", ErrInvalidLimit, v)
}

// Open resolves root to an absolute, symlink-free real path, opens it
// with os.OpenRoot, and returns a Workspace bound to the open root.
// root must exist and be a directory. Open is
// OpenWith(Options{Root: root}), so its reads carry
// DefaultMaxReadBytes. Close the result.
func Open(root string) (*Workspace, error) {
	return OpenWith(Options{Root: root})
}

// OpenWith validates opts and opens a Workspace on opts.Root, the same
// way Open does, under the read bound opts names. It returns
// opts.Validate's error unchanged. Close the result.
func OpenWith(opts Options) (*Workspace, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, fmt.Errorf("workspace: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("workspace: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("workspace: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace: %s is not a directory", resolved)
	}
	r, err := os.OpenRoot(resolved)
	if err != nil {
		return nil, fmt.Errorf("workspace: %w", err)
	}
	limit := opts.MaxReadBytes
	if limit == 0 {
		limit = DefaultMaxReadBytes
	}
	return &Workspace{root: resolved, r: r, maxReadBytes: limit}, nil
}

// Root returns the Workspace's resolved absolute root path.
func (w *Workspace) Root() string {
	return w.root
}

// Close closes the Workspace's open root. Close is idempotent,
// matching os.Root.Close. Every method returns an error matching
// fs.ErrClosed after Close. Close on a nil or zero-value Workspace
// returns nil, so a deferred Close before an error check is safe.
func (w *Workspace) Close() error {
	if w == nil || w.r == nil {
		return nil
	}
	return w.r.Close()
}
