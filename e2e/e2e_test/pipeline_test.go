package e2e_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/e2e"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// pipelinePlan builds one plan holding a sequential step, a
// two-member panel, a two-step sub-workflow, and a two-iteration
// loop. The loop child's Route alternates its final status, so the
// parent never needs a self-row machine.New forbids.
func pipelinePlan(t *testing.T, artifacts *agentrun.Artifacts, parity *int32) *flow.Definition {
	t.Helper()
	subChild, err := flow.New([]flow.Step{
		{ID: "s1", To: "subMid", Payload: "sub-a"},
		{ID: "s2", To: "subFinal", Needs: []string{"s1"}, Payload: "sub-b"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New sub child: %v", err)
	}
	loopChild, err := flow.New([]flow.Step{
		{
			ID: "branch", To: "mid", Payload: "loop-payload",
			Route: func(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
				if atomic.AddInt32(parity, 1)%2 == 1 {
					return []string{"toA"}, nil
				}
				return []string{"toB"}, nil
			},
		},
		{ID: "toA", To: "loopA", Needs: []string{"branch"}, Payload: "loop-a"},
		{ID: "toB", To: "loopB", Needs: []string{"branch"}, Payload: "loop-b"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New loop child: %v", err)
	}
	twice := func(ctx context.Context) (bool, error) {
		st, _ := flow.LoopStateFrom(ctx)
		return st.Iteration == 0, nil
	}
	p, err := flow.New([]flow.Step{
		{ID: "review", To: "reviewed", Payload: "invoice-42"},
		{ID: "p1", To: "paneled", Needs: []string{"review"}, Payload: "panel-one"},
		{ID: "p2", To: "paneled", Needs: []string{"review"}, Payload: "panel-two"},
		{ID: "sub1", To: "unused", Needs: []string{"p1", "p2"}, Sub: subChild, Payload: "sub-parent"},
		{
			ID: "looper", To: "unused2", Needs: []string{"sub1"}, Payload: "loop-parent",
			Sub: loopChild, Loop: &flow.LoopPolicy{Guard: twice},
		},
		{
			ID: "ship", To: "shipped", Needs: []string{"looper"},
			PayloadFrom: agentrun.PayloadOf("review", artifacts),
		},
	}, []flow.Panel{{"p1", "p2"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	return p
}

// pipelineMachine builds every row the plan's walk fires: the child
// rows, the sub-workflow's parent row, the loop's first-fire rows,
// its re-entry rows, and the ship rows.
func pipelineMachine(t *testing.T) *machine.Definition {
	t.Helper()
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "reviewed", Trigger: "t01"},
		machine.Transition{From: "reviewed", To: "paneled", Trigger: "t02"},
		machine.Transition{From: "queued", To: "subMid", Trigger: "t03"},
		machine.Transition{From: "subMid", To: "subFinal", Trigger: "t04"},
		machine.Transition{From: "paneled", To: "subFinal", Trigger: "t05"},
		machine.Transition{From: "queued", To: "mid", Trigger: "t06"},
		machine.Transition{From: "mid", To: "loopA", Trigger: "t07"},
		machine.Transition{From: "mid", To: "loopB", Trigger: "t08"},
		machine.Transition{From: "subFinal", To: "loopA", Trigger: "t09"},
		machine.Transition{From: "subFinal", To: "loopB", Trigger: "t10"},
		machine.Transition{From: "loopA", To: "loopB", Trigger: "t11"},
		machine.Transition{From: "loopB", To: "loopA", Trigger: "t12"},
		machine.Transition{From: "loopA", To: "shipped", Trigger: "t13"},
		machine.Transition{From: "loopB", To: "shipped", Trigger: "t14"},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return m
}

// pipelineRegistry holds one prefix tool per gated step. The panel
// members never reach the ack chain, so they need no tools.
func pipelineRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	reg := tools.New()
	for _, tl := range []tools.Tool{
		e2e.PrefixTool{ToolName: "review", Prefix: "reviewed:"},
		e2e.PrefixTool{ToolName: "sub1", Prefix: "sub1:"},
		e2e.PrefixTool{ToolName: "s1", Prefix: "s1:"},
		e2e.PrefixTool{ToolName: "s2", Prefix: "s2:"},
		e2e.PrefixTool{ToolName: "branch", Prefix: "branch:"},
		e2e.PrefixTool{ToolName: "toA", Prefix: "toA:"},
		e2e.PrefixTool{ToolName: "toB", Prefix: "toB:"},
		e2e.PrefixTool{ToolName: "looper", Prefix: "looper:"},
		e2e.PrefixTool{ToolName: "ship", Prefix: "ship:"},
	} {
		if err := reg.Add(tl); err != nil {
			t.Fatalf("registry.Add: %v", err)
		}
	}
	return reg
}

// TestPipelineRunsEveryPlanShape drives one agentrun run through a
// sequential step, a panel wave, a sub-workflow, and a two-iteration
// loop. It asserts the final status, the artifacts, the stored refs,
// the event counts, and the validator's agreement.
func TestPipelineRunsEveryPlanShape(t *testing.T) {
	ctx := context.Background()
	artifacts := &agentrun.Artifacts{}
	parity := int32(0)
	plan := pipelinePlan(t, artifacts, &parity)
	m := pipelineMachine(t)
	store, err := memory.New(4096)
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}

	// The validator and the runner must agree on this machine.
	if err := agentrun.ValidateMatrix(plan, m); err != nil {
		t.Fatalf("ValidateMatrix = %v, want nil", err)
	}

	rec := e2e.NewRecorder()
	runner, err := agentrun.New(agentrun.Options{
		Agent:     e2eAgent(t, "pipeline-agent", plan),
		Machine:   m,
		Tools:     pipelineRegistry(t),
		Store:     store,
		Artifacts: artifacts,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	for _, name := range []events.Name{
		agent.MessageDeliveredEvent, agent.MessageAckedEvent,
		agent.ThreadVerifiedEvent,
	} {
		if err := runner.Bus().Subscribe(name, rec.Handler()); err != nil {
			t.Fatalf("Subscribe(%s): %v", name, err)
		}
	}

	status, _, err := runner.Run(ctx, "thread-pipeline", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "shipped" {
		t.Fatalf("status = %q, want %q", status, "shipped")
	}
	if got := atomic.LoadInt32(&parity); got != 2 {
		t.Fatalf("branch route calls = %d, want 2", got)
	}
	assertPipelineArtifacts(t, artifacts, store)
	assertPipelineEvents(t, rec)
}

// assertPipelineArtifacts checks every recorded artifact and its
// content-addressed store ref, and that no panel member leaked into
// the ack chain.
func assertPipelineArtifacts(t *testing.T, artifacts *agentrun.Artifacts, store *memory.Store) {
	t.Helper()
	wantArtifacts := map[string]string{
		"review": "reviewed:invoice-42",
		"sub1":   "sub1:sub-parent",
		"looper": "looper:loop-parent",
		"s1":     "s1:sub-a",
		"s2":     "s2:sub-b",
		"branch": "branch:loop-payload",
		"toA":    "toA:loop-a",
		"toB":    "toB:loop-b",
		"ship":   "ship:reviewed:invoice-42",
	}
	for step, want := range wantArtifacts {
		got, ok := artifacts.Get(step)
		if !ok || got != want {
			t.Errorf("artifact %q = %q,%v want %q,true", step, got, ok, want)
		}
		ref := envelope.ContextRef(want)
		blob, err := store.Get(ref)
		if err != nil || string(blob) != want {
			t.Errorf("store ref for %q = %q,%v want the artifact bytes", step, blob, err)
		}
	}
	for _, step := range []string{"p1", "p2"} {
		if _, ok := artifacts.Get(step); ok {
			t.Errorf("panel member %q reached the ack chain; a wave never confirms", step)
		}
	}

}

// assertPipelineEvents checks the delivered, acked, and verified
// event counts and the verified event's terminal position.
func assertPipelineEvents(t *testing.T, rec *e2e.Recorder) {
	t.Helper()
	count := func(name events.Name) int {
		n := 0
		for _, got := range rec.Names() {
			if got == name {
				n++
			}
		}
		return n
	}
	if got := count(agent.MessageDeliveredEvent); got != 10 {
		t.Errorf("delivered events = %d, want 10", got)
	}
	if got := count(agent.MessageAckedEvent); got != 10 {
		t.Errorf("acked events = %d, want 10", got)
	}
	if got := count(agent.ThreadVerifiedEvent); got != 1 {
		t.Errorf("thread-verified events = %d, want 1", got)
	}
	names := rec.Names()
	if last := names[len(names)-1]; last != agent.ThreadVerifiedEvent {
		t.Errorf("last event = %s, want the thread verification", last)
	}
	if names[0] != agent.MessageDeliveredEvent || names[1] != agent.MessageAckedEvent {
		t.Errorf("first events = %s,%s, want a delivered then acked pair", names[0], names[1])
	}
}
