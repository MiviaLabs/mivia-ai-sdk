package runconfig

import (
	"context"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// Runner builds a validated agentrun.Runner from the loaded
// definition. The caller must first set Options.Agent, register the
// document's external tools on External, and set every bound internal
// Kind on Blocks. Runner resolves each binding, builds one
// tools.Registry keyed by step ID, sets Options.Machine and
// Options.Tools, and passes Options to agentrun.New. A nil Agent
// yields agentrun.ErrNoAgent; a missing external tool yields
// ErrUnknownTool; a missing internal Kind yields ErrUnknownInternal.
func (d *Definition) Runner() (*agentrun.Runner, error) {
	reg := tools.New()
	for _, b := range d.Bindings {
		inner, err := d.resolve(b)
		if err != nil {
			return nil, err
		}
		if err := reg.Add(stepTool{step: b.Step, inner: inner}); err != nil {
			return nil, fmt.Errorf("%w: step %q: %s", ErrBadDocument, b.Step, err.Error())
		}
	}
	opts := d.Options
	opts.Machine = d.Machine
	opts.Tools = reg
	return agentrun.New(opts)
}

// resolve resolves one binding to its underlying tool.
func (d *Definition) resolve(b Binding) (tools.Tool, error) {
	if b.Internal {
		t, ok := d.Blocks.get(b.Kind)
		if !ok {
			return nil, fmt.Errorf("%w: step %q needs %q", ErrUnknownInternal, b.Step, b.Kind)
		}
		return t, nil
	}
	t, ok := d.External.Get(b.Tool)
	if !ok {
		return nil, fmt.Errorf("%w: step %q needs %q", ErrUnknownTool, b.Step, b.Tool)
	}
	return t, nil
}

// stepTool adapts one resolved tool to its step ID, so the built ack
// chain runs it by that ID.
type stepTool struct {
	step  string
	inner tools.Tool
}

// Name returns the bound step ID.
func (s stepTool) Name() string { return s.step }

// Run delegates to the resolved tool.
func (s stepTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return s.inner.Run(ctx, in)
}
