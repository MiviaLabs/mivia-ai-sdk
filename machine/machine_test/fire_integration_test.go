package machine_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// twoStepMachine carries the definition and its action capture state.
type twoStepMachine struct {
	d     *machine.Definition
	order *[]string
}

// buildTwoStepMachine builds idle to running to done with moving actions.
// The order slice records each action in run order. The done entry
// action writes the returned output record directly.
func buildTwoStepMachine() twoStepMachine {
	var order []string
	d, err := machine.New(
		machine.Status("idle"),
		machine.Transition{
			From:    machine.Status("idle"),
			To:      machine.Status("running"),
			Trigger: machine.Trigger("start"),
			OnEntry: func(_ context.Context, _ *machine.InOut) error {
				order = append(order, "entry running")
				return nil
			},
		},
		machine.Transition{
			From:    machine.Status("running"),
			To:      machine.Status("done"),
			Trigger: machine.Trigger("finish"),
			Guard: func(_ context.Context) (bool, error) {
				order = append(order, "guard finish")
				return true, nil
			},
			OnExit: func(_ context.Context, _ *machine.InOut) error {
				order = append(order, "exit running")
				return nil
			},
			OnEntry: func(_ context.Context, rec *machine.InOut) error {
				order = append(order, "entry done")
				rec.Output = "result"
				return nil
			},
		},
	)
	if err != nil {
		panic("buildTwoStepMachine: " + err.Error())
	}
	return twoStepMachine{d: d, order: &order}
}

// TestFireAcrossTwoTransitions runs the real path across two moves.
// It proves the exit and entry actions run in exit-then-entry order
// and that the entry action fills the returned output record.
func TestFireAcrossTwoTransitions(t *testing.T) {
	t.Parallel()
	m := buildTwoStepMachine()
	ctx := context.Background()

	// First move: idle to running.
	got, out, err := m.d.Fire(ctx, "idle", "start", machine.InOut{Output: "seed"})
	if err != nil {
		t.Fatalf("first Fire: %v", err)
	}
	if got != "running" {
		t.Fatalf("first Fire status = %q, want %q", got, "running")
	}
	if out.Output != "seed" {
		t.Fatalf("first Fire Output = %q, want %q", out.Output, "seed")
	}
	if out.Input != nil {
		t.Fatalf("first Fire Input = %v, want nil", out.Input)
	}

	// Second move: running to done, through a guard.
	got, out, err = m.d.Fire(ctx, "running", "finish", machine.InOut{Input: "request"})
	if err != nil {
		t.Fatalf("second Fire: %v", err)
	}
	if got != "done" {
		t.Fatalf("second Fire status = %q, want %q", got, "done")
	}
	// The returned record carries the output the entry action wrote.
	if out.Output != "result" {
		t.Fatalf("second Fire Output = %q, want %q", out.Output, "result")
	}

	// The order proves exit runs before entry on the same row.
	want := []string{
		"entry running",
		"guard finish", // immediate before the move
		"exit running", // leave the source status
		"entry done",   // enter the target status
	}
	if !reflect.DeepEqual(*m.order, want) {
		t.Fatalf("action order = %v, want %v", *m.order, want)
	}
}

// TestFireOutputCarriesRecord proves an action-written output round-trips.
func TestFireOutputCarriesRecord(t *testing.T) {
	t.Parallel()
	d, err := machine.New(
		"idle",
		machine.Transition{
			From:    "idle",
			To:      "running",
			Trigger: "start",
			OnEntry: func(_ context.Context, rec *machine.InOut) error {
				rec.Output = "carried"
				return nil
			},
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	in := machine.InOut{Input: 42}
	_, out, err := d.Fire(context.Background(), "idle", "start", in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Input != 42 {
		t.Errorf("Output record Input = %v, want 42", out.Input)
	}
	if out.Output != "carried" {
		t.Errorf("Output record Output = %q, want %q", out.Output, "carried")
	}
}
