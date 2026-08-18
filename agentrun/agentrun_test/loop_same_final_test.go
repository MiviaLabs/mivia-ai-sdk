package agentrun_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestRunLoopedSameFinalChildCompletes proves the composition layer
// carries a loop child with one final: the validator passes without
// a self-row, the tool runs once per iteration, and the artifacts
// keep the latest result with the full run history.
func TestRunLoopedSameFinalChildCompletes(t *testing.T) {
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
	plan, err := flow.New([]flow.Step{
		{ID: "looper", Payload: "p", Sub: child, Loop: &flow.LoopPolicy{Guard: twice}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("start",
		machine.Transition{From: "start", To: "done", Trigger: "t1"})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	if err := agentrun.ValidateMatrix(plan, m); err != nil {
		t.Fatalf("ValidateMatrix = %v, want nil", err)
	}
	calls := 0
	reg := tools.New()
	addTools(t, reg, runCounterTool{calls: &calls},
		prefixTool{name: "looper", prefix: "l:"})
	artifacts := &agentrun.Artifacts{}
	runner, err := agentrun.New(agentrun.Options{
		Agent: mustAgent(t, plan), Machine: m, Tools: reg, Artifacts: artifacts,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	status, _, err := runner.Run(ctx, "thread-same-final", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "done" {
		t.Fatalf("status = %q, want %q", status, "done")
	}
	if calls != 2 {
		t.Fatalf("work tool calls = %d, want 2", calls)
	}
	if got, _ := artifacts.Get("work"); got != "ran:2" {
		t.Errorf("work artifact = %q, want the latest result", got)
	}
	if runs := artifacts.History("work"); len(runs) != 2 {
		t.Errorf("work history = %+v, want both runs", runs)
	}
}

// runCounterTool counts its calls and returns the count.
type runCounterTool struct {
	calls *int
}

// Name returns the registry name.
func (runCounterTool) Name() string { return "work" }

// Run counts the run and returns its ordinal.
func (r runCounterTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	*r.calls++
	return tools.Out{Value: "ran:" + strconv.Itoa(*r.calls)}, nil
}
