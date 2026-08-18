package trace_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/trace"
)

// TestStartOnBareContextYieldsRootSpan pins the root case: no span in
// ctx means a zero ParentID, a non-zero ID, and a returned ctx that
// SpanFrom reads back.
func TestStartOnBareContextYieldsRootSpan(t *testing.T) {
	tr := trace.New()
	ctx, s := tr.Start(context.Background(), "root")
	if s.ParentID != 0 {
		t.Fatalf("root ParentID = %d, want 0", s.ParentID)
	}
	if s.ID == 0 {
		t.Fatal("root ID = 0, want non-zero")
	}
	if s.Name != "root" {
		t.Fatalf("root Name = %q, want %q", s.Name, "root")
	}
	got, ok := trace.SpanFrom(ctx)
	if !ok || got != s {
		t.Fatalf("SpanFrom(returned ctx) = (%p, %v), want (%p, true)", got, ok, s)
	}
}

// TestStartSetsParentFromContext pins the linkage rule: a ctx already
// carrying a span makes the new span its child.
func TestStartSetsParentFromContext(t *testing.T) {
	tr := trace.New()
	parentCtx, parent := tr.Start(context.Background(), "parent")
	_, child := tr.Start(parentCtx, "child")
	if child.ParentID != parent.ID {
		t.Fatalf("child ParentID = %d, want parent ID %d", child.ParentID, parent.ID)
	}
}

// TestStartIssuesDistinctIDs pins the ID rule across a growing count
// of Start calls on one Tracer.
func TestStartIssuesDistinctIDs(t *testing.T) {
	cases := []struct {
		name  string
		count int
	}{
		{name: "two starts", count: 2},
		{name: "eight starts", count: 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := trace.New()
			seen := make(map[trace.SpanID]bool, tc.count)
			ctx := context.Background()
			for i := 0; i < tc.count; i++ {
				_, s := tr.Start(ctx, "span")
				if seen[s.ID] {
					t.Fatalf("Start issued duplicate ID %d at call %d", s.ID, i)
				}
				seen[s.ID] = true
			}
			if len(seen) != tc.count {
				t.Fatalf("distinct IDs = %d, want %d", len(seen), tc.count)
			}
		})
	}
}

// TestSpanFromWithoutSpanReturnsNilFalse pins the empty case: a ctx
// that never saw Start reports no span.
func TestSpanFromWithoutSpanReturnsNilFalse(t *testing.T) {
	s, ok := trace.SpanFrom(context.Background())
	if ok {
		t.Fatal("SpanFrom(bare ctx) ok = true, want false")
	}
	if s != nil {
		t.Fatalf("SpanFrom(bare ctx) = %v, want nil", s)
	}
}
