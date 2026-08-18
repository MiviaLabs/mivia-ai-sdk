package agentrun_test

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// countingPrefixTool counts its calls and the names the chain
// resolved, so a suffixed message still runs the plain tool name.
type countingPrefixTool struct {
	name  string
	mu    sync.Mutex
	names []string
	calls int
}

// Name returns the registry name.
func (t *countingPrefixTool) Name() string { return t.name }

// Run records the call and returns the input payload.
func (t *countingPrefixTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls++
	return tools.Out{Value: "ran"}, nil
}

// TestRunLoopedChildRunsToolPerIteration proves a looped child step
// runs its tool once per iteration under one suffixed thread, and
// each iteration records its own artifact.
func TestRunLoopedChildRunsToolPerIteration(t *testing.T) {
	ctx := context.Background()
	parity := int32(0)
	child, err := flow.New([]flow.Step{
		{
			ID: "branch", To: "mid", Payload: "p",
			Route: func(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
				if atomic.AddInt32(&parity, 1)%2 == 1 {
					return []string{"toA"}, nil
				}
				return []string{"toB"}, nil
			},
		},
		{ID: "toA", To: "a", Needs: []string{"branch"}, Payload: "pa"},
		{ID: "toB", To: "b", Needs: []string{"branch"}, Payload: "pb"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New child: %v", err)
	}
	twice := func(ctx context.Context) (bool, error) {
		st, _ := flow.LoopStateFrom(ctx)
		return st.Iteration == 0, nil
	}
	plan, err := flow.New([]flow.Step{
		{ID: "looper", To: "unused", Payload: "pp",
			Sub: child, Loop: &flow.LoopPolicy{Guard: twice}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("start",
		machine.Transition{From: "start", To: "mid", Trigger: "t1"},
		machine.Transition{From: "mid", To: "a", Trigger: "t2"},
		machine.Transition{From: "mid", To: "b", Trigger: "t3"},
		machine.Transition{From: "start", To: "a", Trigger: "t4"},
		machine.Transition{From: "a", To: "b", Trigger: "t5"},
		machine.Transition{From: "b", To: "a", Trigger: "t6"},
		machine.Transition{From: "start", To: "b", Trigger: "t7"},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	branch := &countingPrefixTool{name: "branch"}
	reg := tools.New()
	addTools(t, reg, branch,
		prefixTool{name: "toA", prefix: "a:"},
		prefixTool{name: "toB", prefix: "b:"},
		prefixTool{name: "looper", prefix: "l:"},
	)
	artifacts := &agentrun.Artifacts{}
	runner, err := agentrun.New(agentrun.Options{
		Agent:     mustAgent(t, plan),
		Machine:   m,
		Tools:     reg,
		Artifacts: artifacts,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	status, _, err := runner.Run(ctx, "thread-loop-tools", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "b" {
		t.Fatalf("status = %q, want %q", status, "b")
	}
	if branch.calls != 2 {
		t.Fatalf("branch tool calls = %d, want 2", branch.calls)
	}
	for _, want := range []string{"branch", "branch#2"} {
		if v, ok := artifacts.Get(want); !ok || !strings.HasPrefix(v, "ran") {
			t.Errorf("artifact %q = %q,%v, want a recorded run", want, v, ok)
		}
	}
}
