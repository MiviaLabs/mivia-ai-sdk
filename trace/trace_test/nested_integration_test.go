package trace_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/trace"
)

// TestNestedSpanTreeLinksThreeLevels wires the primitive the way a
// caller does: each level starts from the ctx the prior Start
// returned, building a root-child-grandchild chain.
func TestNestedSpanTreeLinksThreeLevels(t *testing.T) {
	tr := trace.New()
	rootCtx, root := tr.Start(context.Background(), "root")
	childCtx, child := tr.Start(rootCtx, "child")
	_, grand := tr.Start(childCtx, "grandchild")

	if grand.ParentID != child.ID {
		t.Fatalf("grandchild ParentID = %d, want child ID %d", grand.ParentID, child.ID)
	}
	if child.ParentID != root.ID {
		t.Fatalf("child ParentID = %d, want root ID %d", child.ParentID, root.ID)
	}
	if root.ParentID != 0 {
		t.Fatalf("root ParentID = %d, want 0", root.ParentID)
	}

	grand.End()
	child.End()
	root.End()
	for _, s := range []*trace.Span{root, child, grand} {
		if s.Duration() < 0 {
			t.Fatalf("%q Duration = %v, want non-negative", s.Name, s.Duration())
		}
	}
}
