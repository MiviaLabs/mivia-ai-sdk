// TriggerTool fires a registered trigger from a tool call.

package subagent

import (
	"context"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/trigger"
)

// firedText reports one fired trigger.
const firedText = "fired"

// TriggerTool returns a tool bound to one trigger registry. The
// input string names the trigger; the registry's own action runs.
func TriggerTool(name string, reg *trigger.Registry) tools.Tool {
	return &triggerTool{name: name, reg: reg}
}

// triggerTool adapts one registry to the tools.Tool interface.
type triggerTool struct {
	name string
	reg  *trigger.Registry
}

// Name returns the registry name.
func (t *triggerTool) Name() string { return t.name }

// Run fires the named trigger.
func (t *triggerTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	if err := t.reg.Fire(ctx, stringValue(in)); err != nil {
		return tools.Out{}, err
	}
	return tools.Out{Value: firedText}, nil
}
