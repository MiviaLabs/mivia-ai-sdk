// FlowTool drives a flow plan as one tool call.

package subagent

import (
	"context"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// FlowTool returns a tool that drives plan against m on every call.
// The input string seeds the starting record; the result is the
// walk's final status. A non-nil bus observes one StepCompletedEvent
// per step.
func FlowTool(name string, plan *flow.Definition, m *machine.Definition, bus *events.Bus) tools.Tool {
	return &flowTool{name: name, plan: plan, m: m, bus: bus}
}

// flowTool adapts one plan and machine to the tools.Tool interface.
type flowTool struct {
	name string
	plan *flow.Definition
	m    *machine.Definition
	bus  *events.Bus
}

// Name returns the registry name.
func (f *flowTool) Name() string { return f.name }

// ExecutionProfile declares TimeoutNone: a flow walk runs open-ended
// inside its own step graph, so no registry cap.
func (f *flowTool) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfile{Timeout: tools.TimeoutNone}
}

// Run walks the plan once, confirming every step.
func (f *flowTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	payload, _ := in.Value.(string)
	confirm := func(context.Context, flow.Step) error { return nil }
	report, err := flow.Run(ctx, f.plan, f.m, machine.InOut{Input: payload}, confirm, f.bus, nil)
	if err != nil {
		return tools.Out{}, err
	}
	return tools.Out{Value: string(report.Status())}, nil
}
