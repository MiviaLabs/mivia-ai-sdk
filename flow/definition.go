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
// It rejects an empty ID, a duplicate ID, a missing dependency, a
// panel that names an unknown step, a panel that names one step
// twice, and a panel whose members disagree on To. Kahn's algorithm
// rejects a cycle. After the graph is proven acyclic, New also
// rejects a panel where one member's Needs closure reaches a fellow
// member of the same panel. It rejects a chained step in a panel of
// two or more members. It rejects a Sub nesting depth above eight.
// It deep-copies the input slices so later caller mutation cannot
// change the built graph.
func New(steps []Step, panels []Panel) (*Definition, error) {
	d := &Definition{
		steps:  copySteps(steps),
		panels: copyPanels(panels),
	}
	ids := map[string]int{}
	if err := validateSteps(d.steps, ids); err != nil {
		return nil, err
	}
	if err := validatePanels(d.panels, d.steps, ids); err != nil {
		return nil, err
	}
	if err := validatePanelChains(d.panels, d.steps, ids); err != nil {
		return nil, err
	}
	if err := validateDepth(d.steps); err != nil {
		return nil, err
	}
	roots, err := findRoots(d.steps, ids)
	if err != nil {
		return nil, err
	}
	if err := validatePanelIndependence(d.steps, d.panels, ids); err != nil {
		return nil, err
	}
	d.roots = roots
	return d, nil
}

// copySteps deep-copies the step slice. Each step's Needs slice is
// copied. Each non-nil Sub is copied recursively, including its
// steps, panels, and roots, so later caller mutation cannot change
// the built graph.
func copySteps(steps []Step) []Step {
	out := make([]Step, len(steps))
	for i, s := range steps {
		out[i] = s
		out[i].Needs = append([]string(nil), s.Needs...)
		if s.Sub != nil {
			out[i].Sub = &Definition{
				steps:  copySteps(s.Sub.steps),
				panels: copyPanels(s.Sub.panels),
				roots:  append([]string(nil), s.Sub.roots...),
			}
		}
	}
	return out
}

// validatePanelChains rejects a panel with two or more members if
// any member has a non-nil Sub. A chained step may appear only as a
// singleton or as the sole member of a one-member panel.
func validatePanelChains(panels []Panel, steps []Step, ids map[string]int) error {
	for i, p := range panels {
		if len(p) < 2 {
			continue
		}
		for _, id := range p {
			if steps[ids[id]].Sub != nil {
				return errorf("panel %d: step %q is chained and may not share a panel", i, id)
			}
		}
	}
	return nil
}

// validateDepth rejects any step whose Sub nesting depth exceeds
// eight. Depth is zero for a nil Sub and one plus the maximum child
// depth otherwise.
func validateDepth(steps []Step) error {
	for i := range steps {
		if depth(&steps[i]) > 8 {
			return errorf("step %q: Sub nesting depth exceeds 8", steps[i].ID)
		}
	}
	return nil
}

// depth returns the Sub nesting depth of s. A nil Sub has depth
// zero. A non-nil Sub has depth one plus the maximum depth of its
// child steps.
func depth(s *Step) int {
	if s.Sub == nil {
		return 0
	}
	max := 0
	for i := range s.Sub.steps {
		if d := depth(&s.Sub.steps[i]); d > max {
			max = d
		}
	}
	return 1 + max
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
