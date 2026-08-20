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

// The four cap markers below each add exactly the methods of one
// optional interface. The sixteen inner* types compose them into
// every subset, mirroring the wrapper variants under test. Bit 0 is
// ProfiledTool, bit 1 is ResultBudgetTool, bit 2 is PrivilegedTool,
// bit 3 is SchemaTool.
type profCapT struct{}

func (profCapT) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfile{}
}

type budCapT struct{}

func (budCapT) MaxResultBytes() int { return 1 }

type privCapT struct{}

func (privCapT) Privileged() bool { return true }

type schemaCapT struct{}

func (schemaCapT) ParameterSchema() []byte { return []byte(`{}`) }

func (schemaCapT) DecodeArguments(raw []byte) (tools.InOut, error) {
	return tools.InOut{Value: string(raw)}, nil
}

type inner0000 struct{ stringTool }
type inner0001 struct {
	stringTool
	profCapT
}
type inner0010 struct {
	stringTool
	budCapT
}
type inner0011 struct {
	stringTool
	profCapT
	budCapT
}
type inner0100 struct {
	stringTool
	privCapT
}
type inner0101 struct {
	stringTool
	profCapT
	privCapT
}
type inner0110 struct {
	stringTool
	budCapT
	privCapT
}
type inner0111 struct {
	stringTool
	profCapT
	budCapT
	privCapT
}
type inner1000 struct {
	stringTool
	schemaCapT
}
type inner1001 struct {
	stringTool
	profCapT
	schemaCapT
}
type inner1010 struct {
	stringTool
	budCapT
	schemaCapT
}
type inner1011 struct {
	stringTool
	profCapT
	budCapT
	schemaCapT
}
type inner1100 struct {
	stringTool
	privCapT
	schemaCapT
}
type inner1101 struct {
	stringTool
	profCapT
	privCapT
	schemaCapT
}
type inner1110 struct {
	stringTool
	budCapT
	privCapT
	schemaCapT
}
type inner1111 struct {
	stringTool
	profCapT
	budCapT
	privCapT
	schemaCapT
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
	{"SchemaTool", func(t tools.Tool) bool { _, ok := t.(tools.SchemaTool); return ok }},
}

// innerFor builds the inner tool for subset mask of the probes.
func innerFor(mask int) tools.Tool {
	inner := stringTool{name: "inner", result: "x"}
	switch mask {
	case 0:
		return inner0000{inner}
	case 1:
		return inner0001{inner, profCapT{}}
	case 2:
		return inner0010{inner, budCapT{}}
	case 3:
		return inner0011{inner, profCapT{}, budCapT{}}
	case 4:
		return inner0100{inner, privCapT{}}
	case 5:
		return inner0101{inner, profCapT{}, privCapT{}}
	case 6:
		return inner0110{inner, budCapT{}, privCapT{}}
	case 7:
		return inner0111{inner, profCapT{}, budCapT{}, privCapT{}}
	case 8:
		return inner1000{inner, schemaCapT{}}
	case 9:
		return inner1001{inner, profCapT{}, schemaCapT{}}
	case 10:
		return inner1010{inner, budCapT{}, schemaCapT{}}
	case 11:
		return inner1011{inner, profCapT{}, budCapT{}, schemaCapT{}}
	case 12:
		return inner1100{inner, privCapT{}, schemaCapT{}}
	case 13:
		return inner1101{inner, profCapT{}, privCapT{}, schemaCapT{}}
	case 14:
		return inner1110{inner, budCapT{}, privCapT{}, schemaCapT{}}
	default:
		return inner1111{inner, profCapT{}, budCapT{}, privCapT{}, schemaCapT{}}
	}
}

// TestSpoolToolSchemaForwardsToInner proves the wrapper's SchemaTool
// methods, when present, actually delegate to inner's own
// ParameterSchema and DecodeArguments rather than merely satisfying
// the interface: a wrapper that returned a fixed or empty schema, or
// that ignored raw and returned a fixed InOut, would still pass
// TestSpoolToolInterfaceParity's type-assertion check but fails here.
func TestSpoolToolSchemaForwardsToInner(t *testing.T) {
	for mask := 8; mask < 1<<len(probes); mask++ {
		inner := innerFor(mask)
		innerSchema, ok := inner.(tools.SchemaTool)
		if !ok {
			t.Fatalf("subset %04b: innerFor built a tool without SchemaTool, want the schema bit set", mask)
		}
		sp, err := spool.NewSpool(newFakeStore(), 1<<20)
		if err != nil {
			t.Fatalf("NewSpool: %v", err)
		}
		wrapped, err := spool.SpoolTool("wrapped", 8, sp, inner)
		if err != nil {
			t.Fatalf("SpoolTool: %v", err)
		}
		wrappedSchema, ok := wrapped.(tools.SchemaTool)
		if !ok {
			t.Fatalf("subset %04b: wrapper does not implement SchemaTool, want it to", mask)
		}

		wantSchema := innerSchema.ParameterSchema()
		gotSchema := wrappedSchema.ParameterSchema()
		if string(gotSchema) != string(wantSchema) {
			t.Errorf("subset %04b: ParameterSchema() = %s, want inner's own %s", mask, gotSchema, wantSchema)
		}

		raw := []byte(`distinct-payload-42`)
		wantIn, wantErr := innerSchema.DecodeArguments(raw)
		gotIn, gotErr := wrappedSchema.DecodeArguments(raw)
		if gotErr != wantErr {
			t.Errorf("subset %04b: DecodeArguments error = %v, want %v", mask, gotErr, wantErr)
		}
		if gotIn != wantIn {
			t.Errorf("subset %04b: DecodeArguments = %+v, want inner's own %+v", mask, gotIn, wantIn)
		}
	}
}

func TestSpoolToolInterfaceParity(t *testing.T) {
	for mask := 0; mask < 1<<len(probes); mask++ {
		inner := innerFor(mask)
		sp, err := spool.NewSpool(newFakeStore(), 1<<20)
		if err != nil {
			t.Fatalf("NewSpool: %v", err)
		}
		wrapped, err := spool.SpoolTool("wrapped", 8, sp, inner)
		if err != nil {
			t.Fatalf("SpoolTool: %v", err)
		}
		for _, p := range probes {
			want := p.satisfy(inner)
			if got := p.satisfy(wrapped); got != want {
				t.Errorf("subset %04b: wrapper satisfies %s = %v, want %v", mask, p.name, got, want)
			}
		}
	}
}

// TestSpoolToolParityRunThroughCaps proves every innerFor fixture
// still runs through the wrapper, so the parity fixtures stay honest.
func TestSpoolToolParityRunThroughCaps(t *testing.T) {
	for mask := 0; mask < 1<<len(probes); mask++ {
		sp, err := spool.NewSpool(newFakeStore(), 1<<20)
		if err != nil {
			t.Fatalf("NewSpool: %v", err)
		}
		wrapped, err := spool.SpoolTool("wrapped", 8, sp, innerFor(mask))
		if err != nil {
			t.Fatalf("SpoolTool: %v", err)
		}
		out, err := wrapped.Run(context.Background(), tools.InOut{})
		if err != nil {
			t.Fatalf("subset %04b: Run: %v", mask, err)
		}
		if out.Value != "x" {
			t.Errorf("subset %04b: Out.Value = %v, want inner result", mask, out.Value)
		}
	}
}
