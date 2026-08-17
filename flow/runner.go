package flow

import (
	"context"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// Confirm gates a step's ack. Run calls it after Fire moves the
// status. A nil return means the ack confirmed; the walk advances.
type Confirm func(ctx context.Context, step Step) error

// Run walks the step graph in topological order. Ready steps run in
// declaration order. Run keeps the current status and one record
// through the walk; each step reads the record and writes the next.
// Run rejects a nil d, a nil m, and a nil confirm at entry, before it
// dereferences d or m. It checks d first, then m, then confirm, so a
// nil m never panics inside a d-nil or m-nil check.
func Run(
	ctx context.Context, d *Definition, m *machine.Definition,
	in machine.InOut, confirm Confirm,
) (machine.Status, machine.InOut, error) {
	if d == nil {
		return machine.Status(""), in, errorf("d must not be nil")
	}
	if m == nil {
		return machine.Status(""), in, errorf("m must not be nil")
	}
	if confirm == nil {
		return m.Initial(), in, errorf("confirm must not be nil")
	}

	done := make(map[string]bool, len(d.steps))
	cur := m.Initial()
	rec := in

	for len(done) < len(d.steps) {
		next, ok := nextReady(d.steps, done)
		if !ok {
			// Unreachable when New accepted the graph: New already
			// proves the graph acyclic and every Needs entry
			// resolvable.
			return cur, rec, errorf("no ready step; graph stalled")
		}

		rows := m.AllowedTransitions(cur)
		row, err := pickTransition(rows, machine.Status(next.To))
		if err != nil {
			return cur, rec, errorf("step %q: %w", next.ID, err)
		}

		cur, rec, err = m.Fire(ctx, cur, row.Trigger, rec)
		if err != nil {
			return cur, rec, errorf("step %q: %w", next.ID, err)
		}

		if err := confirm(ctx, next); err != nil {
			return cur, rec, errorf("step %q: ack not confirmed: %w", next.ID, err)
		}

		done[next.ID] = true
	}

	return cur, rec, nil
}

// nextReady scans steps in declaration order for the first step whose
// Needs are all in done and that is not itself in done. Returns false
// when every step is already done.
func nextReady(steps []Step, done map[string]bool) (Step, bool) {
	for _, s := range steps {
		if done[s.ID] {
			continue
		}
		ready := true
		for _, need := range s.Needs {
			if !done[need] {
				ready = false
				break
			}
		}
		if ready {
			return s, true
		}
	}
	return Step{}, false
}

// pickTransition filters rows to the one whose To equals to. Zero
// matches and more than one match both fail; the error names the
// candidate count so the ambiguity is visible.
func pickTransition(rows []machine.Transition, to machine.Status) (machine.Transition, error) {
	var found *machine.Transition
	count := 0
	for i := range rows {
		if rows[i].To == to {
			if found == nil {
				found = &rows[i]
			}
			count++
		}
	}
	switch count {
	case 0:
		return machine.Transition{}, fmt.Errorf(
			"no transition to status %q from %q", to, rowsFrom(rows),
		)
	case 1:
		return *found, nil
	default:
		return machine.Transition{}, fmt.Errorf(
			"ambiguous transition to status %q from %q (%d candidates)",
			to, rowsFrom(rows), count,
		)
	}
}

// rowsFrom returns the shared From status of rows, or the zero
// Status when rows is empty. AllowedTransitions always returns rows
// that share one From, so the first row's From names the source.
func rowsFrom(rows []machine.Transition) machine.Status {
	if len(rows) == 0 {
		return machine.Status("")
	}
	return rows[0].From
}
