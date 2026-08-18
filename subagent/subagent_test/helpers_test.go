package subagent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/e2e"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// prefixRunner builds a one-step runner whose "work" tool returns
// prefix plus the input payload, recording into artifacts.
func prefixRunner(t *testing.T, prefix string, artifacts *agentrun.Artifacts) *agentrun.Runner {
	t.Helper()
	plan, err := flow.New([]flow.Step{
		{ID: "work", To: "done", Payload: "go"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "done", Trigger: "run"},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	reg := tools.New()
	if err := reg.Add(e2e.PrefixTool{ToolName: "work", Prefix: prefix}); err != nil {
		t.Fatalf("registry.Add: %v", err)
	}
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New: %v", err)
	}
	a, err := agent.New(id, discovery.Card{Name: "sub", Capabilities: []string{"c"}}, plan)
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	r, err := agentrun.New(agentrun.Options{
		Agent:     a,
		Machine:   m,
		Tools:     reg,
		Artifacts: artifacts,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	return r
}

// failingRunner builds a runner whose step tool always fails.
func failingRunner(t *testing.T, msg string) *agentrun.Runner {
	t.Helper()
	plan, err := flow.New([]flow.Step{
		{ID: "work", To: "done", Payload: "go"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "done", Trigger: "run"},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	reg := tools.New()
	if err := reg.Add(failTool{name: "work", msg: msg}); err != nil {
		t.Fatalf("registry.Add: %v", err)
	}
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New: %v", err)
	}
	a, err := agent.New(id, discovery.Card{Name: "sub", Capabilities: []string{"c"}}, plan)
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	r, err := agentrun.New(agentrun.Options{Agent: a, Machine: m, Tools: reg})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	return r
}

// runnerOver builds a runner over a ready plan, machine, and
// registry, failing the test on the first error.
func runnerOver(t *testing.T, plan *flow.Definition, m *machine.Definition, reg *tools.Registry) *agentrun.Runner {
	t.Helper()
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New: %v", err)
	}
	a, err := agent.New(id, discovery.Card{Name: "sub", Capabilities: []string{"c"}}, plan)
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	r, err := agentrun.New(agentrun.Options{Agent: a, Machine: m, Tools: reg})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	return r
}

// failTool fails every run with a fixed message.
type failTool struct {
	name string
	msg  string
}

// Name returns the registry name.
func (f failTool) Name() string { return f.name }

// Run returns the fixed failure.
func (f failTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return tools.Out{}, errors.New(f.msg)
}
