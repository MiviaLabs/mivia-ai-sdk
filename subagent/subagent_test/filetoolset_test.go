package subagent_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/secretpath"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/workspace"
)

// TestOpenFileToolsValidatesOptions is the direct pin for the gap
// this phase closes: shipped 718d79b constructors took a bare
// *workspace.Workspace, so nothing stopped a caller from wiring one
// opened with no Deny into a model-facing tool. This row is red
// against that shipped shape, and green once OpenFileTools and its
// mandatory Deny land, since a nil Deny can no longer reach a
// FileTools at all.
func TestOpenFileToolsValidatesOptions(t *testing.T) {
	matcher, err := secretpath.NewMatcher(nil)
	if err != nil {
		t.Fatalf("secretpath.NewMatcher: %v", err)
	}
	root := t.TempDir()

	cases := []struct {
		label string
		opts  subagent.FileToolOptions
	}{
		{"blank root", subagent.FileToolOptions{Root: "", Deny: matcher}},
		{"nil deny", subagent.FileToolOptions{Root: root, Deny: nil}},
		{"blank root and nil deny", subagent.FileToolOptions{Root: "", Deny: nil}},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			ft, err := subagent.OpenFileTools(c.opts)
			if ft != nil {
				t.Fatalf("OpenFileTools() ft = %v, want nil", ft)
			}
			if err == nil {
				t.Fatal("OpenFileTools() error = nil, want non-nil")
			}
			if c.opts.Deny == nil && !errors.Is(err, subagent.ErrDenyRequired) {
				t.Fatalf("OpenFileTools() error = %v, want ErrDenyRequired", err)
			}
		})
	}

	t.Run("valid", func(t *testing.T) {
		ft, err := subagent.OpenFileTools(subagent.FileToolOptions{Root: root, Deny: matcher})
		if err != nil {
			t.Fatalf("OpenFileTools() error = %v, want nil", err)
		}
		if ft == nil {
			t.Fatal("OpenFileTools() ft = nil, want non-nil")
		}
		if err := ft.Close(); err != nil {
			t.Fatalf("Close() error = %v, want nil", err)
		}
	})
}

// TestFileToolsCloseIsIdempotent proves two Close calls on one opened
// FileTools both return nil.
func TestFileToolsCloseIsIdempotent(t *testing.T) {
	ft, _ := openFileTools(t, nil)
	if err := ft.Close(); err != nil {
		t.Fatalf("first Close() error = %v, want nil", err)
	}
	if err := ft.Close(); err != nil {
		t.Fatalf("second Close() error = %v, want nil", err)
	}
}

// TestFileToolsDeniesSecretPath confirms the tool layer forwards
// workspace's already-shipped denial without swallowing it: every
// file tool refuses the denied path with workspace.ErrSecretPath, and
// permits an otherwise-identical sibling path.
func TestFileToolsDeniesSecretPath(t *testing.T) {
	for _, row := range fileToolRows() {
		t.Run(row.label, func(t *testing.T) {
			ft, root := openFileTools(t, []string{"secret.env"})
			if err := os.WriteFile(filepath.Join(root, "secret.env"), []byte("TOKEN=x"), 0o600); err != nil {
				t.Fatalf("seed denied file: %v", err)
			}
			if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o600); err != nil {
				t.Fatalf("seed permitted file: %v", err)
			}

			tool := row.build(ft)
			_, err := tool.Run(context.Background(), tools.InOut{Value: row.args("secret.env")})
			if !errors.Is(err, workspace.ErrSecretPath) {
				t.Fatalf("Run(secret.env) error = %v, want workspace.ErrSecretPath", err)
			}

			_, err = tool.Run(context.Background(), tools.InOut{Value: row.args("a.txt")})
			if errors.Is(err, workspace.ErrSecretPath) {
				t.Fatalf("Run(a.txt) error = %v, want no ErrSecretPath", err)
			}
		})
	}
}

// TestOpenFileToolsSymlinkRefused pins the unconditional symlink
// refusal: a symlink to a permitted file, under a mandatory but
// empty-pattern Deny, still returns workspace.ErrSecretPath through
// WorkspaceReadTool.
func TestOpenFileToolsSymlinkRefused(t *testing.T) {
	ft, root := openFileTools(t, nil)
	if err := os.WriteFile(filepath.Join(root, "real.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("seed real file: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "real.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatalf("os.Symlink: %v", err)
	}

	tool := subagent.WorkspaceReadTool("read", ft, 0)
	_, err := tool.Run(context.Background(), tools.InOut{Value: subagent.WorkspaceReadArgs{Path: "link.txt"}})
	if !errors.Is(err, workspace.ErrSecretPath) {
		t.Fatalf("Run() error = %v, want workspace.ErrSecretPath", err)
	}
}

// TestFileToolsConcurrentReadsSafeUnderClose proves several
// goroutines can call WorkspaceReadTool.Run against one shared,
// still-open FileTools with no race, pinning the "no mutex needed"
// claim: os.Root's methods are safe for concurrent use, and FileTools
// adds no further mutable state.
func TestFileToolsConcurrentReadsSafeUnderClose(t *testing.T) {
	ft, root := openFileTools(t, nil)
	if err := os.WriteFile(filepath.Join(root, "shared.txt"), []byte("shared content"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	tool := subagent.WorkspaceReadTool("read", ft, 0)

	const workers = 8
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			out, err := tool.Run(context.Background(), tools.InOut{Value: subagent.WorkspaceReadArgs{Path: "shared.txt"}})
			if err != nil {
				errs[idx] = err
				return
			}
			if out.Value != "shared content" {
				errs[idx] = errors.New("unexpected content")
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}

	if err := ft.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
}
