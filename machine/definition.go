package machine

import (
	"context"
	"fmt"
)

// Definition holds an initial status and a validated transition table.
// The fields are unexported; the type is immutable after New.
// names carries the wire names Decode read; Encode reads them back.
type Definition struct {
	initial     Status
	transitions []Transition
	names       []transName
}

// Initial returns the initial status of the definition.
func (d Definition) Initial() Status { return d.initial }

// Transitions returns a copy of the transition table.
// The copy keeps the definition immutable; callers cannot mutate the internal table.
func (d Definition) Transitions() []Transition {
	return append([]Transition(nil), d.transitions...)
}

// AllowedTransitions returns all transitions whose From matches from.
// The returned slice is a fresh copy; mutating it cannot affect the definition.
// Returns an empty slice when no transitions match.
func (d Definition) AllowedTransitions(from Status) []Transition {
	// First pass: count the matching rows
	count := 0
	for _, t := range d.transitions {
		if t.From == from {
			count++
		}
	}

	// Allocate exactly one slice of that exact size
	out := make([]Transition, count)
	idx := 0
	for _, t := range d.transitions {
		if t.From == from {
			out[idx] = t
			idx++
		}
	}
	return out
}

// AllowedTriggers returns the distinct triggers available from from.
// Returns an empty slice when no transitions match.
func (d Definition) AllowedTriggers(from Status) []Trigger {
	// First pass: count the matching rows
	count := 0
	for _, t := range d.transitions {
		if t.From == from {
			count++
		}
	}

	// Allocate exactly one slice of that exact size
	out := make([]Trigger, count)
	idx := 0
	for _, t := range d.transitions {
		if t.From == from {
			out[idx] = t.Trigger
			idx++
		}
	}
	return out
}

// New builds a Definition and validates the transition table.
// It rejects an empty transition list. It copies the input slice so
// later caller mutation of ts cannot change the built table.
func New(initial Status, ts ...Transition) (*Definition, error) {
	if len(ts) == 0 {
		return nil, fmt.Errorf("machine: transition list must not be empty")
	}
	d := &Definition{initial: initial, transitions: append([]Transition(nil), ts...)}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return d, nil
}

// Validate checks the transition table for invalid shapes.
// It rejects self loops and transitions whose From is not
// reachable from the initial status through the table.
func (d *Definition) Validate() error {
	if len(d.transitions) == 0 {
		return fmt.Errorf("machine: transition list must not be empty")
	}
	for _, t := range d.transitions {
		if t.From == t.To {
			return fmt.Errorf(
				"machine: self loop from %q to %q is not allowed",
				t.From, t.To,
			)
		}
	}
	reachable := map[Status]bool{d.initial: true}
	for changed := true; changed; {
		changed = false
		for _, t := range d.transitions {
			if reachable[t.From] && !reachable[t.To] {
				reachable[t.To] = true
				changed = true
			}
		}
	}
	for _, t := range d.transitions {
		if !reachable[t.From] {
			return fmt.Errorf(
				"machine: transition from %q is not in the declared set",
				t.From,
			)
		}
	}
	for i := range d.transitions {
		for j := i + 1; j < len(d.transitions); j++ {
			if d.transitions[i].From == d.transitions[j].From &&
				d.transitions[i].Trigger == d.transitions[j].Trigger {
				return fmt.Errorf(
					"machine: duplicate transition from %q on %q",
					d.transitions[i].From, d.transitions[i].Trigger,
				)
			}
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
	for i := range d.transitions {
		if d.transitions[i].From == from && d.transitions[i].Trigger == trig {
			row = &d.transitions[i]
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
