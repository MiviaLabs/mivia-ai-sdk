package flow

import "fmt"

// Definition holds a validated step graph and its panels.
// The fields are unexported; the type is immutable after New.
// roots carries the step IDs with no Needs, in declaration order.
type Definition struct {
	steps  []Step
	panels []Panel
	roots  []string
}

// New builds a Definition and validates the step graph.
// It rejects an empty ID, a duplicate ID, a missing dependency,
// and a panel that names an unknown step. Kahn's algorithm rejects
// a cycle. It deep-copies the input slices so later caller mutation
// cannot change the built graph.
func New(steps []Step, panels []Panel) (*Definition, error) {
	d := &Definition{
		steps:  copySteps(steps),
		panels: copyPanels(panels),
	}
	ids := map[string]int{}
	if err := validateSteps(d.steps, ids); err != nil {
		return nil, err
	}
	if err := validatePanels(d.panels, ids); err != nil {
		return nil, err
	}
	roots, err := findRoots(d.steps, ids)
	if err != nil {
		return nil, err
	}
	d.roots = roots
	return d, nil
}

// copySteps deep-copies the step slice. Each step's Needs slice is
// copied, so later caller mutation cannot change the built graph.
func copySteps(steps []Step) []Step {
	out := make([]Step, len(steps))
	for i, s := range steps {
		out[i] = s
		out[i].Needs = append([]string(nil), s.Needs...)
	}
	return out
}

// Roots returns the root step IDs in declaration order.
// A root is a step with no Needs. The copy keeps the definition
// immutable; callers cannot mutate the internal slice.
func (d Definition) Roots() []string {
	return append([]string(nil), d.roots...)
}

// copyPanels copies the panel slice for immutability.
func copyPanels(panels []Panel) []Panel {
	out := make([]Panel, len(panels))
	for i, p := range panels {
		out[i] = append(Panel(nil), p...)
	}
	return out
}

// errorf wraps a formatted error with the flow package prefix.
func errorf(format string, a ...any) error {
	return fmt.Errorf("flow: "+format, a...)
}
