package provider_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// BenchmarkRunTurnNonStream benchmarks RunTurn against a fake
// Completer in non-streaming mode. The fake does no I/O.
// Target: under one microsecond per call.
// Measured: ~14 ns/op, 0 B/op, 0 allocs/op (the Messages validation
// loop and the Chat dispatch both allocate nothing for this shape).
func BenchmarkRunTurnNonStream(b *testing.B) {
	f := &fakeCompleter{name: "bench", chatResp: provider.Response{
		Message: provider.Message{Role: provider.RoleAssistant, Content: "ok"},
	}}
	req := provider.Request{
		Model:    "bench-model",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hello"}},
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := provider.RunTurn(ctx, f, req); err != nil {
			b.Fatal(err)
		}
	}
}

// TestRunTurnAllocBudget guards the allocation floor for RunTurn's
// non-streaming path over one hundred sequential calls. The measured
// baseline is 0 allocations per call. The budget allows one
// allocation to absorb a small, legitimate change without masking a
// real regression.
func TestRunTurnAllocBudget(t *testing.T) {
	f := &fakeCompleter{name: "bench", chatResp: provider.Response{
		Message: provider.Message{Role: provider.RoleAssistant, Content: "ok"},
	}}
	req := provider.Request{
		Model:    "bench-model",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hello"}},
	}
	ctx := context.Background()
	alloc := testing.AllocsPerRun(100, func() {
		if _, err := provider.RunTurn(ctx, f, req); err != nil {
			t.Fatal(err)
		}
	})
	if alloc > 1 {
		t.Fatalf("RunTurn allocated %v times per call; budget is 1", alloc)
	}
}
