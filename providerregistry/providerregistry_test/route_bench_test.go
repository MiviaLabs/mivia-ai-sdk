package providerregistry_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/providerregistry"
)

// benchRegistry builds a Registry holding two fakes that do no I/O,
// registered in first-succeeds order. It takes testing.TB so the
// benchmark and the allocation budget test share one setup.
func benchRegistry(tb testing.TB) (*providerregistry.Registry, provider.Request) {
	tb.Helper()
	first := &fakeCompleter{name: "alpha", chatResp: provider.Response{
		Message:      provider.Message{Role: provider.RoleAssistant, Content: "ok"},
		FinishReason: "stop",
	}}
	second := &fakeCompleter{name: "beta", chatResp: provider.Response{
		Message:      provider.Message{Role: provider.RoleAssistant, Content: "ok"},
		FinishReason: "stop",
	}}
	r := providerregistry.New()
	if err := r.Register("alpha", first); err != nil {
		tb.Fatal(err)
	}
	if err := r.Register("beta", second); err != nil {
		tb.Fatal(err)
	}
	return r, userRequest()
}

// BenchmarkRouteFirstSucceeds benchmarks Route against two fakes that
// do no I/O, on the first-name-succeeds path.
// Target: under one microsecond per call.
// Measured: ~36 ns/op, 0 B/op, 0 allocs/op (one RLock, one map
// lookup, one RunTurn dispatch to Chat).
func BenchmarkRouteFirstSucceeds(b *testing.B) {
	r, req := benchRegistry(b)
	order := []string{"alpha", "beta"}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Route(ctx, req, order, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// TestRouteAllocBudget guards the allocation floor for Route's
// first-name-succeeds path over one hundred sequential calls. The
// measured baseline is 0 allocations per call. The budget allows one
// allocation to absorb a small, legitimate change without masking a
// real regression.
func TestRouteAllocBudget(t *testing.T) {
	r, req := benchRegistry(t)
	order := []string{"alpha", "beta"}
	ctx := context.Background()
	alloc := testing.AllocsPerRun(100, func() {
		if _, err := r.Route(ctx, req, order, nil); err != nil {
			t.Fatal(err)
		}
	})
	if alloc > 1 {
		t.Fatalf("Route allocated %v times per call; budget is 1", alloc)
	}
}
