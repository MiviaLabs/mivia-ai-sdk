package agentloop_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// BenchmarkRunOneIteration measures one no-tool-call iteration's cost,
// including New's Definitions call. Baseline recorded on this
// change's development machine: roughly 270 ns/op and 6 allocs/op. A
// regression much past that points at an accidental allocation in the
// hot loop path, not a bound this benchmark enforces.
func BenchmarkRunOneIteration(b *testing.B) {
	reg := tools.New()
	msgs := []provider.Message{textMessage(provider.RoleUser, "hi")}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		completer := &scriptedCompleter{responses: []provider.Response{
			{Message: textMessage(provider.RoleAssistant, "hi there")},
		}}
		loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 1})
		if err != nil {
			b.Fatalf("New() error = %v, want nil", err)
		}
		if _, err := loop.Run(context.Background(), msgs); err != nil {
			b.Fatalf("Run() error = %v, want nil", err)
		}
	}
}
