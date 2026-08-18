package mcp

import (
	"context"
	"testing"
)

// BenchmarkCallTool measures CallTool against the in-memory fixture
// server, on a tool that returns immediately with no progress
// notification. The SDK's own JSON-RPC marshaling on both ends of the
// in-memory pipe is the expected allocation source; the budget in
// BenchmarkCallToolAllocs is calibrated to the measured baseline
// below, not a round number.
//
// Measured baseline on the author's machine: ~264 allocs/op.
func BenchmarkCallTool(b *testing.B) {
	server := newFixtureServer(nil)
	addEchoTool(server, "echo")
	c := connectFixture(b, server, ClientOptions{})
	defer c.Close()

	ctx := context.Background()
	args := map[string]any{"message": "bench"}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := c.CallTool(ctx, "echo", args); err != nil {
			b.Fatalf("CallTool: %v", err)
		}
	}
}

// BenchmarkCallToolAllocs asserts CallTool's allocation budget with
// testing.AllocsPerRun, independent of -benchmem's per-op reporting.
func BenchmarkCallToolAllocs(b *testing.B) {
	server := newFixtureServer(nil)
	addEchoTool(server, "echo")
	c := connectFixture(b, server, ClientOptions{})
	defer c.Close()

	ctx := context.Background()
	args := map[string]any{"message": "bench"}

	const budget = 300 // measured baseline ~264 allocs/op; see BenchmarkCallTool.
	got := testing.AllocsPerRun(20, func() {
		if _, err := c.CallTool(ctx, "echo", args); err != nil {
			b.Fatalf("CallTool: %v", err)
		}
	})
	if got > budget {
		b.Fatalf("CallTool allocated %.0f allocs/op, want <= %d", got, budget)
	}
}
