package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/workspace"
)

// BenchmarkReadFile measures one small in-root read. It asserts no
// allocation budget: the syscall dominates, and io.ReadAll grows its
// own buffer.
func BenchmarkReadFile(b *testing.B) {
	dir := b.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hello workspace"), 0o600); err != nil {
		b.Fatalf("WriteFile: %v", err)
	}
	w, err := workspace.Open(dir)
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	b.Cleanup(func() { _ = w.Close() })

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := w.ReadFile("f.txt"); err != nil {
			b.Fatalf("ReadFile: %v", err)
		}
	}
}
