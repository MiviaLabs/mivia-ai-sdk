// Parity test: the SpoolTool wrapper declares all four optional
// interfaces, tools.ProfiledTool, tools.ResultBudgetTool,
// tools.PrivilegedTool, and tools.SchemaTool, for every inner, and it
// mirrors inner's values through the tools helpers. tools.SchemaOf
// still reports nil, false for a schema-less wrapper, because
// tools.SchemaOf fails closed on nil schema bytes; agentloop.New then
// fails with ErrNoSchema naming the wrapper. Enumerates every
// subset of the known interfaces, so a wrapper that dropped a
// capability, or that forwarded a wrong value, fails here, not in a
// live run. When tools gains a new optional interface, add it to
// probes and to SpoolTool in the same change. See docs/plans/spool.md.
package spool_test

import (
	"context"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/spool"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// The four cap markers below each add exactly the methods of one
// optional interface. The sixteen inner* types compose them into
// every subset. Bit 0 is ProfiledTool, bit 1 is ResultBudgetTool,
// bit 2 is PrivilegedTool, bit 3 is SchemaTool.
type profCapT struct{}

// parityProfile is profCapT's distinctive published profile. It
// differs from the zero ExecutionProfile in every field, so a value
// assertion tells a forwarded profile apart from the
// not-implemented default.
var parityProfile = tools.ExecutionProfile{
	Class:       tools.ExecutionClassExternal,
	ResourceKey: "parity-key",
	Timeout:     7 * time.Millisecond,
}

func (profCapT) ExecutionProfile() tools.ExecutionProfile {
	return parityProfile
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

// TestSpoolToolNilSchemaIdentityDecode covers the schema-less masks
// under the one-struct wrapper. The wrapper still implements
// tools.SchemaTool; ParameterSchema returns nil; and DecodeArguments
// identity-decodes raw bytes as a string value. This row kills the
// mutation that drops the comma-ok fallback in DecodeArguments.
func TestSpoolToolNilSchemaIdentityDecode(t *testing.T) {
	for mask := 0; mask < 8; mask++ {
		inner := innerFor(mask)
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
		if got := wrappedSchema.ParameterSchema(); got != nil {
			t.Errorf("subset %04b: ParameterSchema() = %s, want nil", mask, got)
		}
		raw := []byte(`identity-payload-7`)
		wantIn := tools.InOut{Value: string(raw)}
		gotIn, gotErr := wrappedSchema.DecodeArguments(raw)
		if gotErr != nil {
			t.Errorf("subset %04b: DecodeArguments error = %v, want nil", mask, gotErr)
		}
		if gotIn != wantIn {
			t.Errorf("subset %04b: DecodeArguments = %+v, want identity %+v", mask, gotIn, wantIn)
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
			// Presence is unconditional now: the wrapper declares
			// all four optional interfaces for every mask. Value
			// parity, checked below, is what still mirrors inner.
			if got := p.satisfy(wrapped); !got {
				t.Errorf("subset %04b: wrapper satisfies %s = false, want true", mask, p.name)
			}
		}
		assertParityValues(t, mask, inner, wrapped)
	}
}

// assertParityValues checks that wrapped forwards inner's published
// values for every optional interface, and reports each helper's
// documented default when inner declares nothing.
func assertParityValues(t *testing.T, mask int, inner, wrapped tools.Tool) {
	t.Helper()

	wantProfile := tools.ExecutionProfile{}
	if mask&1 != 0 {
		wantProfile = parityProfile
	}
	gotProfile := tools.ExecutionProfileOf(wrapped)
	if gotProfile != wantProfile || gotProfile != tools.ExecutionProfileOf(inner) {
		t.Errorf("subset %04b: ExecutionProfileOf(wrapper) = %+v, want %+v", mask, gotProfile, wantProfile)
	}

	wantBudget := 0
	if mask&2 != 0 {
		wantBudget = 1
	}
	gotBudget, gotBudgetOK := tools.ResultBudgetOf(wrapped)
	if gotBudget != wantBudget || !gotBudgetOK {
		t.Errorf("subset %04b: ResultBudgetOf(wrapper) = %d,%v, want %d,true", mask, gotBudget, gotBudgetOK, wantBudget)
	}

	wantPriv := mask&4 != 0
	if got := tools.IsPrivileged(wrapped); got != wantPriv {
		t.Errorf("subset %04b: IsPrivileged(wrapper) = %v, want %v", mask, got, wantPriv)
	}

	wantSchema, wantSchemaOK := "", false
	if mask&8 != 0 {
		wantSchema, wantSchemaOK = "{}", true
	}
	gotSchema, gotSchemaOK := tools.SchemaOf(wrapped)
	if string(gotSchema) != wantSchema || gotSchemaOK != wantSchemaOK {
		t.Errorf("subset %04b: SchemaOf(wrapper) = %q,%v, want %q,%v", mask, gotSchema, gotSchemaOK, wantSchema, wantSchemaOK)
	}
	if !wantSchemaOK && gotSchema != nil {
		t.Errorf("subset %04b: SchemaOf(wrapper) schema = %v, want nil", mask, gotSchema)
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
