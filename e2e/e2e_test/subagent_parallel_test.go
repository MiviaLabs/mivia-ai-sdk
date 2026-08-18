package e2e_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// fanoutTool is one orchestrator step tool that spawns every spec's
// subagent concurrently through subagent.RunAll and joins their
// statuses. A panel wave cannot do this: waves never reach the ack
// chain where tools run.
type fanoutTool struct {
	specs []subagent.Spec
}

// Name returns the registry name.
func (f *fanoutTool) Name() string { return "fanout" }

// Run spawns all specs at once and returns the joined statuses.
func (f *fanoutTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	results := subagent.RunAll(ctx, f.specs)
	parts := make([]string, 0, len(results))
	for _, r := range results {
		if r.Err != nil {
			return tools.Out{}, r.Err
		}
		parts = append(parts, r.Name+"="+string(r.Status))
	}
	return tools.Out{Value: strings.Join(parts, ",")}, nil
}

// blockedRunner builds a one-step runner whose tool signals label,
// then blocks until release: spawned subagents must overlap.
func blockedRunner(t *testing.T, label string, started chan<- string, release <-chan struct{}) *agentrun.Runner {
	t.Helper()
	plan, err := flow.New([]flow.Step{{ID: "work", To: "done", Payload: "go"}}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "done", Trigger: "run"})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	reg := tools.New()
	if err := reg.Add(labelTool{label: label, started: started, release: release}); err != nil {
		t.Fatalf("registry.Add: %v", err)
	}
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, label, plan), Machine: m, Tools: reg,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	return runner
}

// labelTool signals its label and waits for release.
type labelTool struct {
	label   string
	started chan<- string
	release <-chan struct{}
}

// Name returns the registry name.
func (l labelTool) Name() string { return "work" }

// Run signals start, waits, then succeeds.
func (l labelTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	l.started <- l.label
	<-l.release
	return tools.Out{Value: "ok:" + l.label}, nil
}

// TestSubagentsSpawnInParallel proves one orchestrator step spawns
// several subagents at once and joins their results.
func TestSubagentsSpawnInParallel(t *testing.T) {
	ctx := context.Background()
	started := make(chan string, 2)
	release := make(chan struct{})
	specs := []subagent.Spec{
		{Name: "alpha", Runner: blockedRunner(t, "alpha", started, release)},
		{Name: "beta", Runner: blockedRunner(t, "beta", started, release)},
	}
	plan, err := flow.New([]flow.Step{
		{ID: "fanout", To: "spawned", Payload: "go"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "spawned", Trigger: "run"})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	reg := tools.New()
	if err := reg.Add(&fanoutTool{specs: specs}); err != nil {
		t.Fatalf("registry.Add: %v", err)
	}
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "orchestrator", plan), Machine: m, Tools: reg,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}

	done := make(chan machine.Status, 1)
	go func() {
		status, _, err := runner.Run(ctx, "thread-fanout", machine.InOut{})
		if err != nil {
			t.Errorf("Run: %v", err)
		}
		done <- status
	}()
	first, second := <-started, <-started
	close(release)
	if status := <-done; status != "spawned" {
		t.Fatalf("status = %q, want spawned", status)
	}
	if first == second {
		t.Fatalf("started = %s twice, want two labels", first)
	}
}
