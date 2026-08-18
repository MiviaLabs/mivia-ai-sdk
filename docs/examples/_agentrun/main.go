// Command agentrun walks a two-step pipeline through the agentrun
// composition layer. It builds the same plan, identity, registry, and
// store the composition example builds by hand, then wires all of them
// into one agentrun.Options literal. agentrun.New validates the matrix,
// the tool names, and the option combinations, subscribes the no-op
// event handlers the bus requires, and builds the ack chain that runs
// each gated step's tool, stores its result, and confirms its ack.
// The caller no longer writes an AckWait closure or the subscription
// ritual see docs/examples/agent-composition.md for that older shape.
package main

import (
	"context"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// prefixTool is a registry tool that returns its prefix joined to the
// string payload of the step it runs for. Each step has its own tool,
// so the recorded results stay distinct and deterministic.
type prefixTool struct {
	name   string
	prefix string
}

// Name returns the tool's registry name.
func (t prefixTool) Name() string { return t.name }

// Run returns t.prefix joined to the string input payload.
func (t prefixTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	s, _ := in.Value.(string)
	return tools.Out{Value: t.prefix + s}, nil
}

// buildPlan returns a two-step plan. Step two reads step one's recorded
// artifact through agentrun.PayloadOf, so its own tool sees the step
// one result as its payload.
func buildPlan(artifacts *agentrun.Artifacts) (*flow.Definition, error) {
	return flow.New([]flow.Step{
		{ID: "review", To: "reviewed", Payload: "review invoice 42"},
		{ID: "ship", To: "shipped", Needs: []string{"review"},
			PayloadFrom: agentrun.PayloadOf("review", artifacts)},
	}, nil)
}

// buildAgent builds an Agent over plan under a freshly generated
// identity and a capability card.
func buildAgent(plan *flow.Definition) (*agent.Agent, error) {
	id, err := identity.New()
	if err != nil {
		return nil, err
	}
	card := discovery.Card{
		Name:         "pipeline-agent",
		Capabilities: []string{"invoice.review"},
	}
	return agent.New(id, card, plan)
}

// buildRegistry returns a registry holding one tool per step.
func buildRegistry() *tools.Registry {
	reg := tools.New()
	_ = reg.Add(prefixTool{name: "review", prefix: "reviewed: "})
	_ = reg.Add(prefixTool{name: "ship", prefix: "shipped: "})
	return reg
}

// buildMachine returns the queued-to-review to-ship machine the plan
// targets. Each declared predecessor has exactly one row.
func buildMachine() (*machine.Definition, error) {
	return machine.New(machine.Status("queued"),
		machine.Transition{From: "queued", To: "reviewed", Trigger: "send"},
		machine.Transition{From: "reviewed", To: "shipped", Trigger: "send"},
	)
}

func main() {
	ctx := context.Background()

	artifacts := &agentrun.Artifacts{}
	plan, err := buildPlan(artifacts)
	if err != nil {
		fmt.Println("buildPlan:", err)
		return
	}
	a, err := buildAgent(plan)
	if err != nil {
		fmt.Println("buildAgent:", err)
		return
	}
	reg := buildRegistry()
	store, err := memory.New(4096)
	if err != nil {
		fmt.Println("memory.New:", err)
		return
	}
	m, err := buildMachine()
	if err != nil {
		fmt.Println("buildMachine:", err)
		return
	}

	// One config struct wires the agent, machine, registry, store, and
	// artifact bag into a runnable pipeline. New runs every check the
	// run needs before it builds anything.
	runner, err := agentrun.New(agentrun.Options{
		Agent:     a,
		Machine:   m,
		Tools:     reg,
		Store:     store,
		Artifacts: artifacts,
	})
	if err != nil {
		fmt.Println("agentrun.New:", err)
		return
	}

	status, _, err := runner.Run(ctx, "thread-pipeline-1", machine.InOut{Input: "go"})
	if err != nil {
		fmt.Println("run:", err)
		return
	}

	result, ok := artifacts.Get("ship")
	fmt.Println("final status:", status)
	fmt.Println("ship artifact:", result, ok)
}
