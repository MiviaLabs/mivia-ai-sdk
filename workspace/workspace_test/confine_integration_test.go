package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/workspace"
)

// The two cases below support one structural claim: resolve performs
// no filesystem access, so no window exists between the confinement
// check and the syscall for an attacker to race. Each case asserts one
// content invariant and asserts nothing about the error set. os.Root
// reports a refusal under a concurrent path swap in several forms, so
// an error assertion here would flake.

// raceDeadline bounds each racing case, so the suite stays fast.
const raceDeadline = 200 * time.Millisecond

// raceIterations bounds the workspace-calling goroutine of each racing
// case.
const raceIterations = 500

// skipWithoutSymlinks skips a case on a platform where symlink
// creation needs a privilege.
func skipWithoutSymlinks(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs a privilege on windows")
	}
}

// raceFixture builds a workspace root and an unrelated outside
// directory under one temp directory, and returns both paths.
func raceFixture(t *testing.T) (root, outside string) {
	t.Helper()
	base := t.TempDir()
	root = filepath.Join(base, "root")
	outside = filepath.Join(base, "outside")
	for _, dir := range []string{root, outside} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatalf("Mkdir(%q): %v", dir, err)
		}
	}
	return root, outside
}

// openRaceWorkspace opens a Workspace over root and closes it at the
// end of the test.
func openRaceWorkspace(t *testing.T, root string) *workspace.Workspace {
	t.Helper()
	w, err := workspace.Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

// TestWriteFollowsNoSwappedSymlink races a write against an attacker
// that turns the write's final component into a symlink pointing
// outside the root. The file outside the root must keep its content.
func TestWriteFollowsNoSwappedSymlink(t *testing.T) {
	skipWithoutSymlinks(t)
	root, outside := raceFixture(t)
	if err := os.Mkdir(filepath.Join(root, "staging"), 0o700); err != nil {
		t.Fatalf("Mkdir(staging): %v", err)
	}
	const original = "outside-original"
	target := filepath.Join(outside, "target")
	if err := os.WriteFile(target, []byte(original), 0o600); err != nil {
		t.Fatalf("WriteFile(target): %v", err)
	}
	w := openRaceWorkspace(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), raceDeadline)
	defer cancel()
	linkPath := filepath.Join(root, "staging", "out.txt")

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer cancel()
		for i := 0; i < raceIterations && ctx.Err() == nil; i++ {
			_ = w.WriteFile("staging/out.txt", []byte("payload"), 0o600)
		}
	}()
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			_ = os.Remove(linkPath)
			_ = os.Symlink(target, linkPath)
		}
	}()
	wg.Wait()

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile(target): %v", err)
	}
	if string(got) != original {
		t.Errorf("target content = %q, want %q: a write followed a swapped symlink", got, original)
	}
}

// TestReadFollowsNoSwappedDirectory races a read against an attacker
// that swaps an intermediate directory component for a symlink
// pointing outside the root. No read may return the outside content.
func TestReadFollowsNoSwappedDirectory(t *testing.T) {
	skipWithoutSymlinks(t)
	root, outside := raceFixture(t)
	const insideContent, outsideContent = "inside", "outside"
	dataDir := filepath.Join(root, "data")
	if err := os.Mkdir(dataDir, 0o700); err != nil {
		t.Fatalf("Mkdir(data): %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "secret.txt"), []byte(insideContent), 0o600); err != nil {
		t.Fatalf("WriteFile(inside): %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte(outsideContent), 0o600); err != nil {
		t.Fatalf("WriteFile(outside): %v", err)
	}
	w := openRaceWorkspace(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), raceDeadline)
	defer cancel()
	aside := filepath.Join(root, "data-aside")

	var leaks int
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer cancel()
		for i := 0; i < raceIterations && ctx.Err() == nil; i++ {
			got, err := w.ReadFile("data/secret.txt")
			if err == nil && string(got) == outsideContent {
				leaks++
			}
		}
	}()
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			_ = os.Rename(dataDir, aside)
			_ = os.Symlink(outside, dataDir)
			_ = os.Remove(dataDir)
			_ = os.Rename(aside, dataDir)
		}
	}()
	wg.Wait()

	if leaks != 0 {
		t.Errorf("ReadFile returned outside content %d times, want 0", leaks)
	}
}
