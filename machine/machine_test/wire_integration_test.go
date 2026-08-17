package machine_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestWireRoundTripPreservesTable proves a decoded table survives
// Decode -> Encode -> Decode intact. From, To, and Trigger stay equal;
// a present guard or action stays present.
func TestWireRoundTripPreservesTable(t *testing.T) {
	t.Parallel()
	reg := machine.NewRegistry()
	reg.Guards["ready"] = busyReady
	reg.Actions["started"] = busyStart
	reg.Actions["left"] = busyExit

	wire := []byte(`{"initial":"idle","transitions":[
		{"from":"idle","to":"running","trigger":"start","guard":"ready","on_entry":"started"},
		{"from":"running","to":"done","trigger":"finish","on_exit":"left"}
	]}`)

	d, err := machine.Decode(wire, reg)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	first := d.Transitions()

	// Encode, then decode again to prove the table is preserved.
	re, err := d.Encode(reg)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	d2, err := machine.Decode(re, reg)
	if err != nil {
		t.Fatalf("re-Decode: %v", err)
	}
	second := d2.Transitions()

	if len(first) != len(second) {
		t.Fatalf("transition count %d != %d", len(first), len(second))
	}
	for i := range first {
		if first[i].From != second[i].From || first[i].To != second[i].To || first[i].Trigger != second[i].Trigger {
			t.Fatalf("row %d differs: %#v vs %#v", i, first[i], second[i])
		}
		if (first[i].Guard == nil) != (second[i].Guard == nil) {
			t.Fatalf("row %d guard presence differs", i)
		}
		if (first[i].OnExit == nil) != (second[i].OnExit == nil) {
			t.Fatalf("row %d on_exit presence differs", i)
		}
		if (first[i].OnEntry == nil) != (second[i].OnEntry == nil) {
			t.Fatalf("row %d on_entry presence differs", i)
		}
	}
}

// TestWireRoundTripFires proves the decoded definition actually works:
// the rebound guard passes and the rebound action writes the output.
func TestWireRoundTripFires(t *testing.T) {
	t.Parallel()
	reg := machine.NewRegistry()
	reg.Guards["ready"] = busyReady
	reg.Actions["started"] = busyStart

	d, err := machine.Decode(
		[]byte(`{"initial":"idle","transitions":[{"from":"idle","to":"running","trigger":"start","guard":"ready","on_entry":"started"}]}`),
		reg,
	)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got, out, err := d.Fire(context.Background(), "idle", "start", machine.InOut{})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if got != "running" {
		t.Fatalf("Fire status = %q, want %q", got, "running")
	}
	if out.Output != "started" {
		t.Fatalf("Fire Output = %q, want %q", out.Output, "started")
	}
}

// TestDecodeRejectsBadShape pushes a bad shape and confirms decode fails.
func TestDecodeRejectsBadShape(t *testing.T) {
	t.Parallel()
	reg := machine.NewRegistry()
	_, err := machine.Decode(
		[]byte(`{"initial":"idle","transitions":[{"from":"idle","to":"idle","trigger":"start"}]}`),
		reg,
	)
	if err == nil {
		t.Fatal("expected error for self loop, got nil")
	}
}
