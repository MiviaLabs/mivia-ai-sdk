package runconfig_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/runconfig"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
)

// goldenDoc is the golden document: one external tool, one flow
// internal tool, a two-row machine, and string options.
const goldenDoc = `{
	"machine": {"initial": "queued", "transitions": [
		{"from": "queued", "to": "mid", "trigger": "run"},
		{"from": "mid", "to": "done", "trigger": "finish"}
	]},
	"plan": {"steps": [
		{"id": "first", "to": "mid", "payload": "seed", "tool": "grep"},
		{"id": "second", "needs": ["first"], "to": "done", "payload": "p2", "internal": "flow"}
	]},
	"options": {"room": "platform-team"},
	"tools": ["grep"]
}`

// TestGoldenDocumentRuns loads the golden document, composes the
// caller-side agent, and runs it end to end.
func TestGoldenDocumentRuns(t *testing.T) {
	d := loadDoc(t, goldenDoc)
	if d.Options.Room != "platform-team" {
		t.Fatalf("room = %q", d.Options.Room)
	}

	ctx := context.Background()
	blocks := runconfig.NewBlocks()
	blocks.Set(runconfig.FlowKind, subagent.FlowTool("flow-inner", innerPlan(t), innerMachine(t), nil))
	if err := d.External.Add(stubTool{name: "grep"}); err != nil {
		t.Fatalf("External.Add: %v", err)
	}

	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New: %v", err)
	}
	card := discovery.Card{Name: "golden-agent", Capabilities: []string{"cap"}}
	a, err := agent.New(id, card, d.Plan)
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	d.Blocks = blocks
	d.Options.Agent = a

	runner, err := d.Runner()
	if err != nil {
		t.Fatalf("Runner: %v", err)
	}
	status, _, err := runner.Run(ctx, "thread-golden", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "done" {
		t.Fatalf("status = %q, want done", status)
	}
}

// innerPlan builds the child plan the flow internal tool drives.
func innerPlan(t *testing.T) *flow.Definition {
	t.Helper()
	p, err := flow.New([]flow.Step{
		{ID: "inner", To: "inner-done", Payload: "p"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	return p
}

// innerMachine builds the child machine the flow internal tool
// drives.
func innerMachine(t *testing.T) *machine.Definition {
	t.Helper()
	m, err := machine.New("inner-queued",
		machine.Transition{From: "inner-queued", To: "inner-done", Trigger: "run"},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return m
}
