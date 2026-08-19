package e2e_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/e2e"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// observeSubRunner builds the one-step worker the orchestrator
// spawns and observes.
func observeSubRunner(t *testing.T) *agentrun.Runner {
	t.Helper()
	plan, err := flow.New([]flow.Step{
		{ID: "work", To: "done", Payload: "go"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New sub: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "done", Trigger: "t1"})
	if err != nil {
		t.Fatalf("machine.New sub: %v", err)
	}
	reg := tools.New()
	addTools(t, reg, e2e.PrefixTool{ToolName: "work", Prefix: "ran:"})
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "worker", plan), Machine: m, Tools: reg,
	})
	if err != nil {
		t.Fatalf("agentrun.New sub: %v", err)
	}
	return runner
}

// countName counts one event name's occurrences in names.
func countName(names []events.Name, name events.Name) int {
	n := 0
	for _, got := range names {
		if got == name {
			n++
		}
	}
	return n
}

// TestOrchestratorObservesSpawnedRun proves the orchestrator's bus
// receives the spawned subagent's delivered, acked, and verified
// events live, alongside its own.
func TestOrchestratorObservesSpawnedRun(t *testing.T) {
	ctx := context.Background()
	subRunner := observeSubRunner(t)

	orchPlan, err := flow.New([]flow.Step{
		{ID: "delegate", To: "delegated", Payload: "go"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New orch: %v", err)
	}
	orchMachine, err := machine.New("queued",
		machine.Transition{From: "queued", To: "delegated", Trigger: "t1"})
	if err != nil {
		t.Fatalf("machine.New orch: %v", err)
	}
	// The orchestrator's bus exists only after New, but New needs the
	// delegate tool registered: a placeholder holds the name until
	// the real tool can bind the bus.
	orchReg := tools.New()
	addTools(t, orchReg, okTool{name: "delegate"})
	orch, err := agentrun.New(agentrun.Options{
		Agent:   e2eAgent(t, "orchestrator", orchPlan),
		Machine: orchMachine, Tools: orchReg,
	})
	if err != nil {
		t.Fatalf("agentrun.New orch: %v", err)
	}
	if !orchReg.Remove("delegate") {
		t.Fatal("Remove placeholder: name not held")
	}
	addTools(t, orchReg, subagent.AsTool("delegate", subRunner,
		subagent.ToolOptions{Bus: orch.Bus()}))

	rec := e2e.NewRecorder()
	for _, name := range []events.Name{
		agent.MessageDeliveredEvent, agent.MessageAckedEvent,
		agent.ThreadVerifiedEvent,
	} {
		if err := orch.Bus().Subscribe(name, rec.Handler()); err != nil {
			t.Fatalf("Subscribe(%s): %v", name, err)
		}
	}

	status, _, err := orch.Run(ctx, "thread-observe", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "delegated" {
		t.Fatalf("status = %q, want delegated", status)
	}
	names := rec.Names()
	// The subagent's forwarded thread verification lands alongside
	// the orchestrator's own: two of each event on one bus.
	for name, want := range map[events.Name]int{
		agent.ThreadVerifiedEvent:   2,
		agent.MessageDeliveredEvent: 2,
		agent.MessageAckedEvent:     2,
	} {
		if got := countName(names, name); got != want {
			t.Errorf("%s = %d, want %d", name, got, want)
		}
	}
}

// okTool holds a step name until the real tool replaces it.
type okTool struct{ name string }

// Name returns the registry name.
func (o okTool) Name() string { return o.name }

// Run succeeds without doing anything.
func (o okTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return tools.Out{Value: "ok"}, nil
}
