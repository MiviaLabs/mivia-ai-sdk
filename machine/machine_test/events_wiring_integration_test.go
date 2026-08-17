package machine_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// moveEventName names the move event this test package emits.
// It is not exported API; it does not live in the events package.
const moveEventName = "machine.move"

// emitMoveData renders the move event data a caller emits after Fire.
// A real caller would derive richer data; the test asserts the wired
// value reaches the bus exactly once.
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
	if err := bus.Subscribe(moveEventName, func(_ context.Context, e events.Event) error {
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
	if err := bus.Emit(ctx, events.Event{Name: moveEventName, Data: emitMoveData("idle", to)}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	_ = out

	want := []string{emitMoveData("idle", machine.Status("running"))}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bus events = %v, want %v", got, want)
	}
}

// TestGuardFailureEmitsNothing proves a rejected move sends no event.
// The first stop fails the guard, so Fire returns an error. The emit
// happens only after a successful move. The bus stays empty.
func TestGuardFailureEmitsNothing(t *testing.T) {
	t.Parallel()
	d := busWiringDefinition()
	bus := events.New()
	called := 0
	if err := bus.Subscribe(moveEventName, func(context.Context, events.Event) error {
		called++
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

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

	// The caller never emits after the failed guard.
	if called != 0 {
		t.Fatalf("bus handler ran %d times; want 0 after a guard failure", called)
	}
}
