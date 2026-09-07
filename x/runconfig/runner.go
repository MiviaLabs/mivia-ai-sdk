package runconfig

import (
	"context"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
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
	for _, id := range subParentIDs(d.Plan) {
		if err := reg.Add(newStepTool(id, passThroughTool{})); err != nil {
			return nil, fmt.Errorf("%w: step %q: %s", ErrBadDocument, id, err.Error())
		}
	}
	opts := d.Options
	opts.Machine = d.Machine
	opts.Tools = reg
	return run.New(opts)
}

// subParentIDs collects the identifiers of every gated step that
// carries a child plan. It mirrors the gating walk in workflow/run:
// the panel set is computed per plan level, and a member of a
// two-or-more-member panel is skipped together with its whole subtree,
// because that member never reaches confirmation.
func subParentIDs(d *flow.Definition) []string {
	if d == nil {
		return nil
	}
	var out []string
	big := bigPanelMembers(d)
	for _, s := range d.Steps() {
		if big[s.ID] {
			continue
		}
		if s.Sub != nil {
			out = append(out, s.ID)
			out = append(out, subParentIDs(s.Sub)...)
		}
	}
	return out
}

// bigPanelMembers returns the step identifiers named in a panel of two
// or more members at this plan level.
func bigPanelMembers(d *flow.Definition) map[string]bool {
	m := map[string]bool{}
	for _, p := range d.Panels() {
		if len(p) >= 2 {
			for _, id := range p {
				m[id] = true
			}
		}
	}
	return m
}

// passThroughTool answers a parent step's confirmation by returning its
// payload unchanged. It publishes no parameter schema, so the ack chain
// skips argument decoding and the plain payload reaches Run.
type passThroughTool struct{}

// Name reports the placeholder name newStepTool replaces with the
// bound step identifier.
func (passThroughTool) Name() string { return "sub" }

// Run returns the string payload unchanged.
func (passThroughTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	v, _ := in.Value.(string)
	return tools.Out{Value: v}, nil
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
