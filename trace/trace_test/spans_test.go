package trace_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/trace"
)

// TestSpansEmptyBeforeStart proves a fresh Tracer reports a non-nil,
// empty slice.
func TestSpansEmptyBeforeStart(t *testing.T) {
	spans := trace.New().Spans()
	if spans == nil {
		t.Fatal("Spans() = nil, want non-nil")
	}
	if len(spans) != 0 {
		t.Fatalf("Spans() length = %d, want 0", len(spans))
	}
}

// TestSpansReturnsStartOrder proves Spans lists every started span in
// start order with the issued IDs.
func TestSpansReturnsStartOrder(t *testing.T) {
	tr := trace.New()
	ctx := context.Background()
	for _, name := range []string{"a", "b", "c"} {
		_, span := tr.Start(ctx, name)
		span.End()
	}
	spans := tr.Spans()
	if len(spans) != 3 {
		t.Fatalf("Spans() length = %d, want 3", len(spans))
	}
	for i, want := range []string{"a", "b", "c"} {
		if spans[i].Name != want {
			t.Errorf("spans[%d].Name = %q, want %q", i, spans[i].Name, want)
		}
		if spans[i].ID != trace.SpanID(i+1) {
			t.Errorf("spans[%d].ID = %d, want %d", i, spans[i].ID, i+1)
		}
	}
}

// TestSpansCopyIsIndependent proves mutating the returned slice never
// changes the next Spans result.
func TestSpansCopyIsIndependent(t *testing.T) {
	tr := trace.New()
	_, span := tr.Start(context.Background(), "root")
	span.End()
	first := tr.Spans()
	first[0] = nil
	second := tr.Spans()
	if len(second) != 1 || second[0] == nil || second[0].Name != "root" {
		t.Fatalf("second Spans() = %+v, want the untouched root span", second)
	}
}
