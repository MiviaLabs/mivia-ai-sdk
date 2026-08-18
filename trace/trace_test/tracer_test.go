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

// TestStartIssuesSequentialIDs pins the ID rule across a growing count
// of Start calls on one Tracer: the first ID is one, and every later
// ID is strictly greater.
func TestStartIssuesSequentialIDs(t *testing.T) {
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
			ctx := context.Background()
			var prev trace.SpanID
			for i := 0; i < tc.count; i++ {
				_, s := tr.Start(ctx, "span")
				if s.ID == 0 {
					t.Fatalf("Start at call %d issued ID 0", i)
				}
				if i == 0 && s.ID != 1 {
					t.Fatalf("first ID = %d, want 1", s.ID)
				}
				if i > 0 && s.ID <= prev {
					t.Fatalf("ID %d at call %d not above prior %d", s.ID, i, prev)
				}
				prev = s.ID
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
