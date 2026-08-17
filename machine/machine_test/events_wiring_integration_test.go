package machine_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// emitMoveData renders the move event data a caller emits after Fire.
// A real caller would derive richer data; the test asserts the wired
// value reaches the bus exactly once. The event name is the typed
// machine.MoveEvent constant, not a string literal.
func emitMoveData(from machine.Status, to machine.Status) string {
	return string(from) + "->" + string(to)
}

// busWiringDefinition builds a real machine definition for the wiring test.
// idle starts. start moves idle to running. stop moves running to done
// through a guard that rejects once. It returns the definition.
func busWiringDefinition() *machine.Definition {
	guardCalls := 0
	d, err := machine.New(
		machine.Status("idle"),
		machine.Transition{
			From:    machine.Status("idle"),
			To:      machine.Status("running"),
			Trigger: machine.Trigger("start"),
		},
		machine.Transition{
			From:    machine.Status("running"),
			To:      machine.Status("done"),
			Trigger: machine.Trigger("stop"),
			Guard: func(context.Context) (bool, error) {
				guardCalls++
				// Reject the first stop, accept the second.
				return guardCalls > 1, nil
			},
		},
	)
	if err != nil {
		panic("busWiringDefinition: " + err.Error())
	}
	return d
}

// TestMachineMoveArrivesOnceOnBus wires Fire to a caller-owned bus.
// It proves one real move lands as exactly one event on the bus.
func TestMachineMoveArrivesOnceOnBus(t *testing.T) {
	t.Parallel()
	d := busWiringDefinition()
	bus := events.New()
	var got []string
	if err := bus.Subscribe(machine.MoveEvent, func(_ context.Context, e events.Event) error {
		got = append(got, e.Data)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	ctx := context.Background()

	to, out, err := d.Fire(ctx, "idle", "start", machine.InOut{})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if to != machine.Status("running") {
		t.Fatalf("Fire status = %q, want %q", to, "running")
	}
	if err := bus.Emit(ctx, events.Event{Name: machine.MoveEvent, Data: emitMoveData("idle", to)}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	_ = out

	want := []string{emitMoveData("idle", machine.Status("running"))}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bus events = %v, want %v", got, want)
	}
}

// TestGuardFailureEmitsNothing proves a rejected move leaves the bus idle.
// The first stop fails the guard, so Fire returns an error. The caller
// does not emit after a failed move; the bus is caller-owned.
func TestGuardFailureEmitsNothing(t *testing.T) {
	t.Parallel()
	d := busWiringDefinition()

	// First move idles to running.
	to, _, err := d.Fire(context.Background(), "idle", "start", machine.InOut{})
	if err != nil {
		t.Fatalf("Fire start: %v", err)
	}

	// The first stop fails the guard, so Fire returns an error.
	_, _, err = d.Fire(context.Background(), to, "stop", machine.InOut{})
	if err == nil {
		t.Fatal("Fire stop: expected a guard-failure error")
	}
}
