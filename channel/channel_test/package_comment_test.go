package channel_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestPackageCommentReferencesNDJSONNotifier proves the package
// comment in channel.go acknowledges the shipped NewNDJSONNotifier
// transport. The comment is part of the package's public surface and
// must stay in sync with the actual API.
func TestPackageCommentReferencesNDJSONNotifier(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	src := filepath.Join(filepath.Dir(file), "..", "channel.go")

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, src, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("ParseFile(%q) error = %v", src, err)
	}
	if f.Doc == nil {
		t.Fatal("channel.go has no package comment")
	}
	if !strings.Contains(f.Doc.Text(), "NewNDJSONNotifier") {
		t.Fatalf("package comment does not reference NewNDJSONNotifier:\n%s", f.Doc.Text())
	}
}
