package flow_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestLoopSameFinalChildReentersWithoutRow proves a loop child that
// ends every iteration on one status re-enters without a self-row:
// the parent's standing already matches the child final, so the
// re-entry fires no transition. The parent's own confirm still fires
// exactly once, after the loop exits.
func TestLoopSameFinalChildReentersWithoutRow(t *testing.T) {
	ctx := context.Background()
	child, err := flow.New([]flow.Step{
		{ID: "work", To: "done", Payload: "w"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New child: %v", err)
	}
	twice := func(ctx context.Context) (bool, error) {
		st, _ := flow.LoopStateFrom(ctx)
		return st.Iteration == 0, nil
	}
	def, err := flow.New([]flow.Step{
		{ID: "looper", Payload: "p", Sub: child, Loop: &flow.LoopPolicy{Guard: twice}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	// The machine holds no done-to-done row; the same-final re-entry
	// must need none.
	m, err := machine.New("start",
		machine.Transition{From: "start", To: "done", Trigger: "t1"})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	confirms := 0
	confirm := func(ctx context.Context, step flow.Step) error {
		confirms++
		return nil
	}
	report, err := flow.Run(ctx, def, m, machine.InOut{}, confirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Status() != "done" {
		t.Fatalf("status = %q, want %q", report.Status(), "done")
	}
	// Two child confirmations, one per iteration, plus the parent's.
	if confirms != 3 {
		t.Fatalf("confirmations = %d, want 3", confirms)
	}
}
