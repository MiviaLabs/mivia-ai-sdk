package workspace

import (
	"fmt"
	"io"
)

// ReadFile reads the file at path, relative to the Workspace's root,
// under the Workspace's own read bound. It is ReadFileLimit(path, 0)
// and adds no rule of its own.
func (w *Workspace) ReadFile(path string) ([]byte, error) {
	return w.ReadFileLimit(path, 0)
}

// ReadFileLimit reads the file at path, relative to the Workspace's
// root, under a per-call bound. A zero limit uses the Workspace's
// MaxReadBytes, a positive limit replaces it, up or down, and
// Unbounded removes it for this call only. Any other value returns
// ErrInvalidLimit and opens no file. A file longer than the effective
// bound returns ErrTooLarge and no bytes. A read of a directory
// returns the raw filesystem error.
func (w *Workspace) ReadFileLimit(path string, limit int64) ([]byte, error) {
	effective, err := w.effectiveLimit(limit)
	if err != nil {
		return nil, err
	}
	resolved, err := w.resolve(path)
	if err != nil {
		return nil, err
	}
	f, err := w.r.Open(resolved)
	if err != nil {
		return nil, classify(path, err)
	}
	defer func() { _ = f.Close() }()
	if effective == Unbounded {
		return io.ReadAll(f)
	}
	// The limit+1 read is what separates a file exactly at the bound
	// from one over it; a read of limit bytes cannot tell the two
	// apart. maxReadLimit keeps the increment from overflowing.
	data, err := io.ReadAll(io.LimitReader(f, effective+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > effective {
		return nil, fmt.Errorf("%w: %s: limit %d bytes", ErrTooLarge, path, effective)
	}
	return data, nil
}

// effectiveLimit maps a per-call limit onto the bound one read uses.
// It calls validateLimit first, so an invalid value refuses before any
// path work. A zero limit takes the Workspace's bound; any other
// passing value stands as given.
func (w *Workspace) effectiveLimit(limit int64) (int64, error) {
	if err := validateLimit(limit); err != nil {
		return 0, err
	}
	if limit == 0 {
		return w.maxReadBytes, nil
	}
	return limit, nil
}
