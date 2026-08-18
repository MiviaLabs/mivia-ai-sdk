package trace_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/trace"
)

// BenchmarkStartEnd benchmarks Tracer.Start immediately followed by
// (*Span).End, the cheapest complete span.
// Target: at most two allocations per run.
// Measured: ~121 ns/op, 160 B/op, 2 allocs/op (the Span value and the
// context.WithValue node; End alone allocates nothing).
func BenchmarkStartEnd(b *testing.B) {
	tr := trace.New()
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		spanCtx, s := tr.Start(ctx, "bench")
		s.End()
		_ = spanCtx
	}
}

// TestStartEndAllocBudget guards the allocation ceiling for Start
// plus End. The baseline is two allocations: the Span value and the
// context.WithValue node. The budget is exact; a third allocation is
// a regression, not headroom.
func TestStartEndAllocBudget(t *testing.T) {
	tr := trace.New()
	ctx := context.Background()
	alloc := testing.AllocsPerRun(100, func() {
		spanCtx, s := tr.Start(ctx, "budget")
		s.End()
		_ = spanCtx
	})
	if alloc > 2 {
		t.Fatalf("Start plus End allocated %v times per run; budget is 2", alloc)
	}
}

// BenchmarkStartSetAttributeEnd benchmarks Start, one SetAttribute
// call, and End together.
// Target: at most three allocations per run.
// Measured: ~137 ns/op, 192 B/op, 3 allocs/op (the two from Start plus
// the attribute store's first backing array).
func BenchmarkStartSetAttributeEnd(b *testing.B) {
	tr := trace.New()
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		spanCtx, s := tr.Start(ctx, "bench")
		s.SetAttribute("key", "value")
		s.End()
		_ = spanCtx
	}
}

// TestStartSetAttributeEndAllocBudget guards the allocation ceiling
// for Start plus one SetAttribute plus End. The baseline is three
// allocations: the two from Start plus the attribute store's first
// backing array. The budget is exact; a fourth allocation is a
// regression.
func TestStartSetAttributeEndAllocBudget(t *testing.T) {
	tr := trace.New()
	ctx := context.Background()
	alloc := testing.AllocsPerRun(100, func() {
		spanCtx, s := tr.Start(ctx, "budget")
		s.SetAttribute("key", "value")
		s.End()
		_ = spanCtx
	})
	if alloc > 3 {
		t.Fatalf("Start, SetAttribute, End allocated %v times per run; budget is 3", alloc)
	}
}
