package machine

import (
	"context"
	"fmt"
)

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

// Fire moves a record from from through the row selected by trig.
// It runs the guard, then OnExit, then OnEntry, in that order.
// It returns the target status and the record in in.
// An action writes the output record through the InOut it receives.
// A nil Guard or a nil Action is checked, never invoked.
// Fire does not run OnExit when the guard fails.
func (d *Definition) Fire(
	ctx context.Context, from Status, trig Trigger, in InOut,
) (Status, InOut, error) {
	var row *Transition
	for i := range d.Transitions {
		if d.Transitions[i].From == from && d.Transitions[i].Trigger == trig {
			row = &d.Transitions[i]
			break
		}
	}
	if row == nil {
		return from, in, fmt.Errorf(
			"machine: no transition from %q on %q",
			from, trig,
		)
	}
	if row.Guard != nil {
		ok, err := row.Guard(ctx)
		if err != nil {
			return from, in, err
		}
		if !ok {
			return from, in, fmt.Errorf(
				"machine: guard rejected move from %q on %q",
				from, trig,
			)
		}
	}
	rec := in
	if row.OnExit != nil {
		if err := row.OnExit(ctx, &rec); err != nil {
			return from, in, err
		}
	}
	if row.OnEntry != nil {
		if err := row.OnEntry(ctx, &rec); err != nil {
			return from, in, err
		}
	}
	return row.To, rec, nil
}
