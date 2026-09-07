package runconfig

import (
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/workflow/run"
)

// Runner builds a validated run.Runner from the loaded
// definition. The caller must first set Options.Agent, register the
// document's external tools on External, and ensure every bound
// internal Kind is on Blocks: Load sets the wireable Kinds the
// document's internal section declares, and the caller sets the
// caller-built Kinds. Runner resolves each binding, builds one
// tools.Registry keyed by step ID, sets Options.Machine and
// Options.Tools, and passes Options to run.New. A nil Agent
// yields run.ErrInvalidOptions; a missing external tool yields
// ErrUnknownTool; a missing internal Kind yields ErrUnknownInternal.
func (d *Definition) Runner() (*run.Runner, error) {
	reg := tools.New()
	for _, b := range d.Bindings {
		inner, err := d.resolve(b)
		if err != nil {
			return nil, err
		}
		if err := reg.Add(newStepTool(b.Step, inner)); err != nil {
			return nil, fmt.Errorf("%w: step %q: %s", ErrBadDocument, b.Step, err.Error())
		}
	}
	opts := d.Options
	opts.Machine = d.Machine
	opts.Tools = reg
	return run.New(opts)
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
