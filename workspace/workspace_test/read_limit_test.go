package workspace_test

import (
	"bytes"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/workspace"
)

// writeSized writes a file of exactly n bytes outside the package API,
// so every bound case knows the file's exact size.
func writeSized(t *testing.T, dir, name string, n int64) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("OpenFile(%q): %v", name, err)
	}
	chunk := bytes.Repeat([]byte("a"), 64<<10)
	for written := int64(0); written < n; {
		size := min(int64(len(chunk)), n-written)
		if _, err := f.Write(chunk[:size]); err != nil {
			t.Fatalf("Write(%q): %v", name, err)
		}
		written += size
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close(%q): %v", name, err)
	}
	return path
}

// openLimited opens a Workspace over a fresh temp directory under the
// named read bound.
func openLimited(t *testing.T, limit int64) (*workspace.Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	w, err := workspace.OpenWith(workspace.Options{Root: dir, MaxReadBytes: limit})
	if err != nil {
		t.Fatalf("OpenWith: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w, dir
}

// TestOptionsValidate pins the one rule validateLimit enforces, plus
// the blank-root refusal. A Validate that accepts every MaxReadBytes
// passes no row below.
func TestOptionsValidate(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name    string
		opts    workspace.Options
		wantErr error
		ok      bool
	}{
		{name: "blank root", opts: workspace.Options{}, ok: false},
		{name: "blank root with valid limit", opts: workspace.Options{MaxReadBytes: 8}, ok: false},
		{name: "negative limit", opts: workspace.Options{Root: dir, MaxReadBytes: -2}, wantErr: workspace.ErrInvalidLimit},
		{name: "limit over maxReadLimit", opts: workspace.Options{Root: dir, MaxReadBytes: math.MaxInt64}, wantErr: workspace.ErrInvalidLimit},
		{name: "unbounded", opts: workspace.Options{Root: dir, MaxReadBytes: workspace.Unbounded}, ok: true},
		{name: "zero selects the default", opts: workspace.Options{Root: dir}, ok: true},
		{name: "positive limit", opts: workspace.Options{Root: dir, MaxReadBytes: 8}, ok: true},
		{name: "limit at maxReadLimit", opts: workspace.Options{Root: dir, MaxReadBytes: math.MaxInt64 - 1}, ok: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.opts.Validate()
			if tc.ok {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Validate() = nil, want error")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("Validate() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestDefaultReadLimit proves the zero value is bounded, not
// unbounded: Open sets no limit, so DefaultMaxReadBytes applies.
func TestDefaultReadLimit(t *testing.T) {
	dir := t.TempDir()
	writeSized(t, dir, "big.txt", workspace.DefaultMaxReadBytes+1)
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	if _, err := w.ReadFile("big.txt"); !errors.Is(err, workspace.ErrTooLarge) {
		t.Errorf("ReadFile(oversized) error = %v, want ErrTooLarge", err)
	}
}

// TestUnboundedOptIn reads the same oversized file through the
// explicit opt-out.
func TestUnboundedOptIn(t *testing.T) {
	w, dir := openLimited(t, workspace.Unbounded)
	size := workspace.DefaultMaxReadBytes + 1
	writeSized(t, dir, "big.txt", size)

	got, err := w.ReadFile("big.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if int64(len(got)) != size {
		t.Errorf("ReadFile length = %d, want %d", len(got), size)
	}
}

// TestConfiguredLimit pins a caller-set bound in both directions.
func TestConfiguredLimit(t *testing.T) {
	w, dir := openLimited(t, 8)
	writeSized(t, dir, "fits.txt", 8)
	writeSized(t, dir, "over.txt", 9)

	if _, err := w.ReadFile("fits.txt"); err != nil {
		t.Errorf("ReadFile(8 bytes under an 8-byte bound): %v", err)
	}
	if _, err := w.ReadFile("over.txt"); !errors.Is(err, workspace.ErrTooLarge) {
		t.Errorf("ReadFile(9 bytes under an 8-byte bound) error = %v, want ErrTooLarge", err)
	}
}

// TestExactLimit is the case the limit+1 read exists for: a file of
// exactly the bound succeeds and returns every byte.
func TestExactLimit(t *testing.T) {
	w, dir := openLimited(t, 8)
	path := writeSized(t, dir, "exact.txt", 8)
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile: %v", err)
	}

	got, err := w.ReadFile("exact.txt")
	if err != nil {
		t.Fatalf("ReadFile(exactly at the bound): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("ReadFile = %q, want %q", got, want)
	}
}

// TestOneByteOver is the other half of the limit+1 read: one byte over
// the bound refuses and returns no bytes.
func TestOneByteOver(t *testing.T) {
	w, dir := openLimited(t, 8)
	writeSized(t, dir, "over.txt", 9)

	got, err := w.ReadFile("over.txt")
	if !errors.Is(err, workspace.ErrTooLarge) {
		t.Errorf("ReadFile(one byte over the bound) error = %v, want ErrTooLarge", err)
	}
	if got != nil {
		t.Errorf("ReadFile(one byte over the bound) = %d bytes, want nil", len(got))
	}
}

// TestPerCallOverrideRaises pins that a positive per-call limit
// replaces a lower workspace bound.
func TestPerCallOverrideRaises(t *testing.T) {
	w, dir := openLimited(t, 8)
	writeSized(t, dir, "f.txt", 64)

	got, err := w.ReadFileLimit("f.txt", 128)
	if err != nil {
		t.Fatalf("ReadFileLimit(path, 128): %v", err)
	}
	if len(got) != 64 {
		t.Errorf("ReadFileLimit length = %d, want 64", len(got))
	}
}

// TestPerCallOverrideLowers pins the same override in the other
// direction.
func TestPerCallOverrideLowers(t *testing.T) {
	w, dir := openLimited(t, 4096)
	writeSized(t, dir, "f.txt", 64)

	if _, err := w.ReadFileLimit("f.txt", 8); !errors.Is(err, workspace.ErrTooLarge) {
		t.Errorf("ReadFileLimit(path, 8) error = %v, want ErrTooLarge", err)
	}
}

// TestPerCallUnbounded removes the bound for one call on a
// default-bounded workspace.
func TestPerCallUnbounded(t *testing.T) {
	dir := t.TempDir()
	size := workspace.DefaultMaxReadBytes + 1
	writeSized(t, dir, "big.txt", size)
	w, err := workspace.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	if _, err := w.ReadFile("big.txt"); !errors.Is(err, workspace.ErrTooLarge) {
		t.Fatalf("ReadFile(oversized) error = %v, want ErrTooLarge", err)
	}
	got, err := w.ReadFileLimit("big.txt", workspace.Unbounded)
	if err != nil {
		t.Fatalf("ReadFileLimit(path, Unbounded): %v", err)
	}
	if int64(len(got)) != size {
		t.Errorf("ReadFileLimit length = %d, want %d", len(got), size)
	}
}

// TestPerCallZeroUsesWorkspaceLimit pins ReadFile as
// ReadFileLimit(path, 0) on both a passing and a failing file.
func TestPerCallZeroUsesWorkspaceLimit(t *testing.T) {
	w, dir := openLimited(t, 8)
	writeSized(t, dir, "fits.txt", 8)
	writeSized(t, dir, "over.txt", 9)

	for _, name := range []string{"fits.txt", "over.txt"} {
		gotDirect, errDirect := w.ReadFile(name)
		gotZero, errZero := w.ReadFileLimit(name, 0)
		if !bytes.Equal(gotDirect, gotZero) {
			t.Errorf("ReadFile(%q) = %q, ReadFileLimit(%q, 0) = %q", name, gotDirect, name, gotZero)
		}
		if (errDirect == nil) != (errZero == nil) {
			t.Errorf("ReadFile(%q) error = %v, ReadFileLimit(%q, 0) error = %v", name, errDirect, name, errZero)
		}
		if errDirect != nil && !errors.Is(errZero, workspace.ErrTooLarge) {
			t.Errorf("ReadFileLimit(%q, 0) error = %v, want ErrTooLarge", name, errZero)
		}
	}
}

// TestInvalidLimit pins that an invalid bound refuses before any path
// work and before any open. The escape row proves the limit check runs
// before resolve; the closed-workspace row proves it runs before the
// open, because an open on a closed root reports fs.ErrClosed.
func TestInvalidLimit(t *testing.T) {
	w, dir := openLimited(t, 8)
	writeSized(t, dir, "f.txt", 4)

	cases := []struct {
		name  string
		path  string
		limit int64
	}{
		{name: "negative", path: "f.txt", limit: -2},
		{name: "over maxReadLimit", path: "f.txt", limit: math.MaxInt64},
		{name: "escaping path loses to the limit", path: "../outside.txt", limit: -2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := w.ReadFileLimit(tc.path, tc.limit)
			if !errors.Is(err, workspace.ErrInvalidLimit) {
				t.Fatalf("ReadFileLimit(%q, %d) error = %v, want ErrInvalidLimit", tc.path, tc.limit, err)
			}
			if errors.Is(err, workspace.ErrEscape) {
				t.Errorf("ReadFileLimit(%q, %d) error = %v, want no ErrEscape", tc.path, tc.limit, err)
			}
			if got != nil {
				t.Errorf("ReadFileLimit(%q, %d) = %d bytes, want nil", tc.path, tc.limit, len(got))
			}
		})
	}

	t.Run("opens no file", func(t *testing.T) {
		closed, closedDir := openLimited(t, 8)
		writeSized(t, closedDir, "f.txt", 4)
		if err := closed.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		_, err := closed.ReadFileLimit("f.txt", -2)
		if !errors.Is(err, workspace.ErrInvalidLimit) {
			t.Fatalf("ReadFileLimit on a closed workspace = %v, want ErrInvalidLimit", err)
		}
		if errors.Is(err, fs.ErrClosed) {
			t.Errorf("ReadFileLimit = %v: the invalid limit opened a file", err)
		}
	})

	t.Run("OpenWith returns the sentinel unchanged", func(t *testing.T) {
		if _, err := workspace.OpenWith(workspace.Options{Root: dir, MaxReadBytes: -2}); !errors.Is(err, workspace.ErrInvalidLimit) {
			t.Errorf("OpenWith(MaxReadBytes: -2) error = %v, want ErrInvalidLimit", err)
		}
	})
}

// TestSecretPathBeatsLimit pins the order between the deny check and
// the bound. The deny check needs no open file, so it answers first.
func TestSecretPathBeatsLimit(t *testing.T) {
	root := t.TempDir()
	writeSized(t, root, ".env", 64)
	w, err := workspace.OpenWith(workspace.Options{
		Root:         root,
		MaxReadBytes: 1,
		Deny:         newMatcher(t, []string{".env"}),
	})
	if err != nil {
		t.Fatalf("OpenWith: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	_, err = w.ReadFile(".env")
	if !errors.Is(err, workspace.ErrSecretPath) {
		t.Fatalf("ReadFile(.env) error = %v, want ErrSecretPath", err)
	}
	if errors.Is(err, workspace.ErrTooLarge) {
		t.Errorf("ReadFile(.env) error = %v, want no ErrTooLarge", err)
	}
}

// TestEscapeBeatsLimit pins that a bound cannot be measured on a path
// the confinement already refuses.
func TestEscapeBeatsLimit(t *testing.T) {
	w, _ := openLimited(t, 1)

	_, err := w.ReadFile("../outside.txt")
	if !errors.Is(err, workspace.ErrEscape) {
		t.Fatalf("ReadFile(escaping) error = %v, want ErrEscape", err)
	}
	if errors.Is(err, workspace.ErrTooLarge) {
		t.Errorf("ReadFile(escaping) error = %v, want no ErrTooLarge", err)
	}
}

// TestOverLimitClosesFile pins the deferred Close on the ErrTooLarge
// path. It asserts no growth in the process descriptor count over 200
// refusals, not exact equality: the runtime opens and closes
// descriptors of its own.
func TestOverLimitClosesFile(t *testing.T) {
	const fdDir = "/proc/self/fd"
	if _, err := os.Stat(fdDir); err != nil {
		t.Skipf("%s is absent: %v", fdDir, err)
	}
	w, dir := openLimited(t, 1)
	writeSized(t, dir, "over.txt", 100)

	if _, err := w.ReadFile("over.txt"); !errors.Is(err, workspace.ErrTooLarge) {
		t.Fatalf("warm-up ReadFile error = %v, want ErrTooLarge", err)
	}
	before := countEntries(t, fdDir)
	for i := 0; i < 200; i++ {
		if _, err := w.ReadFile("over.txt"); !errors.Is(err, workspace.ErrTooLarge) {
			t.Fatalf("ReadFile iteration %d error = %v, want ErrTooLarge", i, err)
		}
	}
	after := countEntries(t, fdDir)
	if after > before {
		t.Errorf("descriptor count grew from %d to %d over 200 refused reads", before, after)
	}
}

// countEntries counts the entries in dir.
func countEntries(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", dir, err)
	}
	return len(entries)
}
