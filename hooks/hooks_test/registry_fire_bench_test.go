package hooks_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
)

// benchRegistry builds a Registry holding n always-allowing handlers
// at PointPreTool. It takes testing.TB so the benchmarks and the
// allocation budget test share one setup.
func benchRegistry(tb testing.TB, n int) *hooks.Registry {
	tb.Helper()
	allow := func(context.Context, any) (bool, error) { return true, nil }
	r := hooks.New()
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("hook-%02d", i)
		if err := r.Add(hooks.PointPreTool, name, allow); err != nil {
			tb.Fatal(err)
		}
	}
	return r
}

// BenchmarkFireTenHandlers benchmarks Fire over ten registered,
// always-allowing handlers.
// Measured: ~17 ns/op, 0 B/op, 0 allocs/op (one lock, one map read,
// one slice walk, ten handler calls).
func BenchmarkFireTenHandlers(b *testing.B) {
	r := benchRegistry(b, 10)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := r.Fire(ctx, hooks.PointPreTool, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFireOneHandler benchmarks Fire over one registered,
// always-allowing handler, the floor for comparison.
// Measured: ~11 ns/op, 0 B/op, 0 allocs/op.
func BenchmarkFireOneHandler(b *testing.B) {
	r := benchRegistry(b, 1)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := r.Fire(ctx, hooks.PointPreTool, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// TestFireAllocBudget guards the allocation floor for one Fire call
// over ten handlers. The measured baseline is 0 allocations per call:
// Fire reads one map entry and walks one slice. The budget allows one
// allocation to absorb a small, legitimate change without masking a
// real regression.
func TestFireAllocBudget(t *testing.T) {
	r := benchRegistry(t, 10)
	ctx := context.Background()
	alloc := testing.AllocsPerRun(100, func() {
		if err := r.Fire(ctx, hooks.PointPreTool, nil); err != nil {
			t.Fatal(err)
		}
	})
	if alloc > 1 {
		t.Fatalf("Fire allocated %v times per call; budget is 1", alloc)
	}
}
