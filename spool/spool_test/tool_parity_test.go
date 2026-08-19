// Parity test: the SpoolTool wrapper implements exactly the optional
// tools interfaces inner implements, no more and no less. Enumerates
// every subset of the known interfaces, so a variant missed by the
// combinatorial switch in SpoolTool fails here, not in a live run.
// When tools gains a new optional interface, add it to probes and to
// SpoolTool's switch in the same change. See docs/plans/spool.md.
package spool_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/spool"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// The three cap markers below each add exactly one optional
// interface method. The eight inner* types compose them into every
// subset, mirroring the wrapper variants under test.
type profCapT struct{}

func (profCapT) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfile{}
}

type budCapT struct{}

func (budCapT) MaxResultBytes() int { return 1 }

type privCapT struct{}

func (privCapT) Privileged() bool { return true }

type innerPlain struct{ stringTool }

type innerProf struct {
	stringTool
	profCapT
}

type innerBud struct {
	stringTool
	budCapT
}

type innerPriv struct {
	stringTool
	privCapT
}

type innerProfBud struct {
	stringTool
	profCapT
	budCapT
}

type innerProfPriv struct {
	stringTool
	profCapT
	privCapT
}

type innerBudPriv struct {
	stringTool
	budCapT
	privCapT
}

type innerAll struct {
	stringTool
	profCapT
	budCapT
	privCapT
}

// probes pairs each optional interface with a satisfied check on a
// tools.Tool value. Probe bit i in the subset mask matches probe i.
var probes = []struct {
	name    string
	satisfy func(tools.Tool) bool
}{
	{"ProfiledTool", func(t tools.Tool) bool { _, ok := t.(tools.ProfiledTool); return ok }},
	{"ResultBudgetTool", func(t tools.Tool) bool { _, ok := t.(tools.ResultBudgetTool); return ok }},
	{"PrivilegedTool", func(t tools.Tool) bool { _, ok := t.(tools.PrivilegedTool); return ok }},
}

// innerFor builds the inner tool for subset mask of the probes.
func innerFor(mask int) tools.Tool {
	inner := stringTool{name: "inner", result: "x"}
	switch mask {
	case 0:
		return innerPlain{inner}
	case 1:
		return innerProf{inner, profCapT{}}
	case 2:
		return innerBud{inner, budCapT{}}
	case 3:
		return innerProfBud{inner, profCapT{}, budCapT{}}
	case 4:
		return innerPriv{inner, privCapT{}}
	case 5:
		return innerProfPriv{inner, profCapT{}, privCapT{}}
	case 6:
		return innerBudPriv{inner, budCapT{}, privCapT{}}
	default:
		return innerAll{inner, profCapT{}, budCapT{}, privCapT{}}
	}
}

func TestSpoolToolInterfaceParity(t *testing.T) {
	for mask := 0; mask < 1<<len(probes); mask++ {
		inner := innerFor(mask)
		wrapped := spool.SpoolTool("wrapped", 8, newFakeStore(), inner)
		for _, p := range probes {
			want := p.satisfy(inner)
			if got := p.satisfy(wrapped); got != want {
				t.Errorf("subset %03b: wrapper satisfies %s = %v, want %v", mask, p.name, got, want)
			}
		}
	}
}

// TestSpoolToolParityRunThroughCaps proves every innerFor fixture
// still runs through the wrapper, so the parity fixtures stay honest.
func TestSpoolToolParityRunThroughCaps(t *testing.T) {
	for mask := 0; mask < 1<<len(probes); mask++ {
		wrapped := spool.SpoolTool("wrapped", 8, newFakeStore(), innerFor(mask))
		out, err := wrapped.Run(context.Background(), tools.InOut{})
		if err != nil {
			t.Fatalf("subset %03b: Run: %v", mask, err)
		}
		if out.Value != "x" {
			t.Errorf("subset %03b: Out.Value = %v, want inner result", mask, out.Value)
		}
	}
}
