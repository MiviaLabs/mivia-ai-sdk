package runconfig

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/heartbeat"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// kindPin is one row of the constructor pin: the Kind, its side of
// the wireable/caller-built partition, a reference builder that calls
// the subagent constructor by hand, and one valid command input for
// behavior parity. Caller-built rows leave the last two fields zero.
type kindPin struct {
	kind     Kind
	wireable bool
	ref      func(d *Definition) (tools.Tool, error)
	cmd      string
}

// refWireable returns a reference builder calling the constructor by
// hand. Every wireable row pins exactly one subagent constructor.
func refWireable(f func(d *Definition) (tools.Tool, error)) func(d *Definition) (tools.Tool, error) {
	return f
}

// conformanceDefinition builds a Definition with a minimal plan and
// machine, so the flow builder can bind them.
func conformanceDefinition(t *testing.T) *Definition {
	t.Helper()
	plan, err := flow.New([]flow.Step{{ID: "only", To: "done", Payload: "p"}}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "done", Trigger: "run"})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return &Definition{Plan: plan, Machine: m}
}

// sameKindSet reports whether a and b hold the same Kind keys.
func sameKindSet(a, b map[Kind]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// pinPartition asserts the twelve-Kind partition is complete: every
// Kind constant is wireable or caller-built, nothing both, nothing
// neither. An undecided new Kind fails here.
func pinPartition(t *testing.T, pins []kindPin) {
	t.Helper()
	table := map[Kind]bool{}
	wireable := map[Kind]bool{}
	caller := map[Kind]bool{}
	for _, p := range pins {
		if table[p.kind] {
			t.Fatalf("kind %q listed twice", p.kind)
		}
		table[p.kind] = true
		if p.wireable {
			wireable[p.kind] = true
		} else {
			caller[p.kind] = true
		}
	}
	if !sameKindSet(table, kinds) {
		t.Fatalf("pin table covers %v, kinds map holds %v; a new Kind needs a decision", table, kinds)
	}
	built := map[Kind]bool{}
	for k := range builders {
		built[k] = true
	}
	if !sameKindSet(built, wireable) {
		t.Fatalf("builders table covers %v, want the wireable set %v", built, wireable)
	}
	if !sameKindSet(callerBuilt, caller) {
		t.Fatalf("callerBuilt holds %v, want the caller-built set %v", callerBuilt, caller)
	}
}

// pinBuilderParity pins each wireable Kind to its hand-called
// constructor. Type parity: fmt.Sprintf("%T") must match. Behavior
// parity: both tools must answer one command input identically.
func pinBuilderParity(t *testing.T, pins []kindPin, d *Definition, cfg wireInternal) {
	t.Helper()
	for _, p := range pins {
		if !p.wireable {
			continue
		}
		got, err := builders[p.kind](d, cfg)
		if err != nil {
			t.Fatalf("%s: builder: %v", p.kind, err)
		}
		want, err := p.ref(d)
		if err != nil {
			t.Fatalf("%s: reference: %v", p.kind, err)
		}
		gotT, wantT := fmt.Sprintf("%T", got), fmt.Sprintf("%T", want)
		if gotT != wantT {
			t.Fatalf("%s: builder built %s, reference built %s", p.kind, gotT, wantT)
		}
		outGot, errGot := got.Run(context.Background(), tools.InOut{Value: p.cmd})
		outWant, errWant := want.Run(context.Background(), tools.InOut{Value: p.cmd})
		if (errGot == nil) != (errWant == nil) {
			t.Fatalf("%s: errors differ: builder %v, reference %v", p.kind, errGot, errWant)
		}
		if errGot != nil && errGot.Error() != errWant.Error() {
			t.Fatalf("%s: error texts differ: builder %q, reference %q", p.kind, errGot, errWant)
		}
		if outGot.Value != outWant.Value {
			t.Fatalf("%s: outputs differ: builder %v, reference %v", p.kind, outGot.Value, outWant.Value)
		}
	}
}

// TestKindBuildersPinConstructors is the conformance pin between the
// document's internal section and subagent's constructors. A rewire
// or an undecided new Kind fails here.
func TestKindBuildersPinConstructors(t *testing.T) {
	pins := []kindPin{
		{DiscoveryKind, true, refWireable(func(*Definition) (tools.Tool, error) {
			return subagent.DiscoveryTool(string(DiscoveryKind)), nil
		}),
			`{"op":"match","card":"{\"name\":\"c1\",\"capabilities\":[\"cap\"]}","need":"cap"}`},
		{FlowKind, true, refWireable(func(d *Definition) (tools.Tool, error) {
			return subagent.FlowTool(string(FlowKind), d.Plan, d.Machine, nil), nil
		}),
			""},
		{HeartbeatKind, true, refWireable(func(*Definition) (tools.Tool, error) {
			monitor, err := heartbeat.New(30 * time.Second)
			if err != nil {
				return nil, err
			}
			return subagent.HeartbeatTool(string(HeartbeatKind), monitor), nil
		}),
			`{"op":"beat","id":"w1"}`},
		{LedgerKind, true, refWireable(func(*Definition) (tools.Tool, error) {
			l, err := ledger.New(ledger.NewMemStore(), nil)
			if err != nil {
				return nil, err
			}
			return subagent.LedgerTool(string(LedgerKind), l, ledger.Actor("a1"), time.Minute), nil
		}),
			`{"op":"state","key":"k1"}`},
		{MemoryKind, true, refWireable(func(*Definition) (tools.Tool, error) {
			store, err := memory.New(1024)
			if err != nil {
				return nil, err
			}
			return subagent.MemoryTool(string(MemoryKind), store), nil
		}),
			`{"op":"put","data":"x"}`},
		{RoomKind, true, refWireable(func(*Definition) (tools.Tool, error) {
			r, err := room.New("room-1", "f1")
			if err != nil {
				return nil, err
			}
			return subagent.RoomTool(string(RoomKind), r, "a1"), nil
		}),
			`{"op":"ismember","id":"nobody"}`},
		{AsToolKind, false, nil, ""},
		{ChannelKind, false, nil, ""},
		{ProviderKind, false, nil, ""},
		{ProviderRegistryKind, false, nil, ""},
		{SchedulerKind, false, nil, ""},
		{TriggerKind, false, nil, ""},
	}
	pinPartition(t, pins)
	cfg := wireInternal{
		Timeout: "30s", MaxBytes: 1024,
		ID: "room-1", Founder: "f1", Actor: "a1", Lease: "1m",
	}
	pinBuilderParity(t, pins, conformanceDefinition(t), cfg)
}
