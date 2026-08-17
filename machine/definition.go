package machine

import "fmt"

// Definition holds an initial status and a validated transition table.
type Definition struct {
	Initial     Status
	Transitions []Transition
}

// New builds a Definition and validates the transition table.
// It rejects an empty transition list.
func New(initial Status, ts ...Transition) (*Definition, error) {
	if len(ts) == 0 {
		return nil, fmt.Errorf("machine: transition list must not be empty")
	}
	d := &Definition{Initial: initial, Transitions: ts}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return d, nil
}

// Validate checks the transition table for invalid shapes.
// It rejects self loops and transitions whose From is not
// reachable from the initial status through the table.
func (d *Definition) Validate() error {
	if len(d.Transitions) == 0 {
		return fmt.Errorf("machine: transition list must not be empty")
	}
	for _, t := range d.Transitions {
		if t.From == t.To {
			return fmt.Errorf(
				"machine: self loop from %q to %q is not allowed",
				t.From, t.To,
			)
		}
	}
	reachable := map[Status]bool{d.Initial: true}
	for changed := true; changed; {
		changed = false
		for _, t := range d.Transitions {
			if reachable[t.From] && !reachable[t.To] {
				reachable[t.To] = true
				changed = true
			}
		}
	}
	for _, t := range d.Transitions {
		if !reachable[t.From] {
			return fmt.Errorf(
				"machine: transition from %q is not in the declared set",
				t.From,
			)
		}
	}
	return nil
}
