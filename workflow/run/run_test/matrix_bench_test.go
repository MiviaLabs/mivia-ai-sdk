package run_test

import (
	"fmt"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/workflow/run"
)

// BenchmarkValidateMatrix benchmarks ValidateMatrix over a synthetic
// chain of one thousand steps backed by one thousand machine rows: a
// fresh checkRow call per step, the shape the row-lookup cost
// multiplies over. Target: under fifteen milliseconds per call.
// Measured baseline, with checkRow copying the whole table through
// machine.Transitions on every call: ~14.3 ms/op, ~74.7 MB/op.
// Measured with the precomputed row counts: ~5.1 ms/op, ~1.2 MB/op.
func BenchmarkValidateMatrix(b *testing.B) {
	const n = 1000
	steps := make([]flow.Step, 0, n)
	rows := make([]machine.Transition, 0, n)
	steps = append(steps, flow.Step{ID: "r0000", To: "s0000"})
	rows = append(rows, tr("queued", "s0000", "t0000"))
	for i := 1; i < n; i++ {
		steps = append(steps, flow.Step{
			ID:    fmt.Sprintf("r%04d", i),
			To:    fmt.Sprintf("s%04d", i),
			Needs: []string{fmt.Sprintf("r%04d", i-1)},
		})
		rows = append(rows, tr(
			fmt.Sprintf("s%04d", i-1),
			fmt.Sprintf("s%04d", i),
			fmt.Sprintf("t%04d", i),
		))
	}
	plan, err := flow.New(steps, nil)
	if err != nil {
		b.Fatalf("flow.New() unexpected error: %v", err)
	}
	m, err := machine.New("queued", rows...)
	if err != nil {
		b.Fatalf("machine.New() unexpected error: %v", err)
	}
	if err := run.ValidateMatrix(plan, m); err != nil {
		b.Fatalf("ValidateMatrix() unexpected error: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := run.ValidateMatrix(plan, m); err != nil {
			b.Fatal(err)
		}
	}
}
