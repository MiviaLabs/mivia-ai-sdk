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

// streamBenchChunks builds a fresh chunk slice per call: drainStream
// only reads, but fakeCompleter's goroutine ranges over the slice
// concurrently with the next call's setup, so each iteration gets its
// own backing array.
func streamBenchChunks() []provider.Chunk {
	return []provider.Chunk{
		{Delta: "Hel"},
		{ToolCallDelta: &provider.ToolCall{Index: 0, ID: "call-0", Name: "search", Arguments: []byte(`{"q":`)}},
		{Delta: "lo, "},
		{ToolCallDelta: &provider.ToolCall{Index: 0, Arguments: []byte(`"cats"}`)}},
		{Delta: "world", Done: true, FinishReason: "tool_calls"},
	}
}

// BenchmarkRunTurnStream benchmarks RunTurn against a fake Completer
// in streaming mode, draining plain-text deltas merged with one
// tool-call fragment. This exercises drainStream, mergeToolCallDelta,
// and buildResponse, the allocation-heavy half of RunTurn that
// BenchmarkRunTurnNonStream does not reach.
// Target: under ten microseconds per call.
// Measured: ~1.5 us/op, ~1264 B/op, 16 allocs/op (the goroutine, map,
// slice, and strings.Builder growth for the merged tool call and
// content).
func BenchmarkRunTurnStream(b *testing.B) {
	req := provider.Request{Model: "bench-model", Stream: true}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f := &fakeCompleter{name: "bench", streamChunks: streamBenchChunks()}
		if _, err := provider.RunTurn(ctx, f, req); err != nil {
			b.Fatal(err)
		}
	}
}

// TestRunTurnStreamAllocBudget guards the allocation floor for
// RunTurn's streaming path over fifty sequential drains, each with one
// merged tool call. The measured baseline is 16 allocations per call.
// The budget of 22 allows a margin of six to absorb a small,
// legitimate change without masking a real regression.
func TestRunTurnStreamAllocBudget(t *testing.T) {
	req := provider.Request{Model: "bench-model", Stream: true}
	ctx := context.Background()
	alloc := testing.AllocsPerRun(50, func() {
		f := &fakeCompleter{name: "bench", streamChunks: streamBenchChunks()}
		if _, err := provider.RunTurn(ctx, f, req); err != nil {
			t.Fatal(err)
		}
	})
	if alloc > 22 {
		t.Fatalf("RunTurn streaming path allocated %v times per call; budget is 22", alloc)
	}
}
