package subagent_test

import (
	"context"
	"errors"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/internal/e2e"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/workflow"
	"github.com/MiviaLabs/mivia-ai-sdk/workflow/run"
)

// prefixRunner builds a one-step runner whose "work" tool returns
// prefix plus the input payload, recording into artifacts.
func prefixRunner(t *testing.T, prefix string, artifacts *run.Artifacts) *run.Runner {
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
	id, err := envelope.New()
	if err != nil {
		t.Fatalf("envelope.New: %v", err)
	}
	a, err := workflow.New(id, flow.Card{Name: "sub", Capabilities: []string{"c"}}, plan)
	if err != nil {
		t.Fatalf("workflow.New: %v", err)
	}
	r, err := run.New(run.Options{
		Agent:     a,
		Machine:   m,
		Tools:     reg,
		Artifacts: artifacts,
	})
	if err != nil {
		t.Fatalf("run.New: %v", err)
	}
	return r
}

// failingRunner builds a runner whose step tool always fails.
func failingRunner(t *testing.T, msg string) *run.Runner {
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
	id, err := envelope.New()
	if err != nil {
		t.Fatalf("envelope.New: %v", err)
	}
	a, err := workflow.New(id, flow.Card{Name: "sub", Capabilities: []string{"c"}}, plan)
	if err != nil {
		t.Fatalf("workflow.New: %v", err)
	}
	r, err := run.New(run.Options{Agent: a, Machine: m, Tools: reg})
	if err != nil {
		t.Fatalf("run.New: %v", err)
	}
	return r
}

// runnerOver builds a runner over a ready plan, machine, and
// registry, failing the test on the first error.
func runnerOver(t *testing.T, plan *flow.Definition, m *machine.Definition, reg *tools.Registry) *run.Runner {
	t.Helper()
	id, err := envelope.New()
	if err != nil {
		t.Fatalf("envelope.New: %v", err)
	}
	a, err := workflow.New(id, flow.Card{Name: "sub", Capabilities: []string{"c"}}, plan)
	if err != nil {
		t.Fatalf("workflow.New: %v", err)
	}
	r, err := run.New(run.Options{Agent: a, Machine: m, Tools: reg})
	if err != nil {
		t.Fatalf("run.New: %v", err)
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
