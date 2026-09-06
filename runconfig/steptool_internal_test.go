package runconfig

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// fullCapTool implements every optional tools.Tool capability
// interface: tools.SchemaTool, tools.ProfiledTool,
// tools.ResultBudgetTool, and tools.PrivilegedTool.
type fullCapTool struct{ name string }

func (t fullCapTool) Name() string { return t.name }
func (t fullCapTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return tools.Out{Value: "ran"}, nil
}
func (t fullCapTool) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfile{Class: tools.ExecutionClassWrite}
}
func (t fullCapTool) MaxResultBytes() int { return 128 }
func (t fullCapTool) Privileged() bool    { return true }
func (t fullCapTool) ParameterSchema() []byte {
	return []byte(`{"type":"object"}`)
}
func (t fullCapTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	return tools.InOut{Value: string(raw)}, nil
}

// bareTool implements none of the optional tools.Tool capability
// interfaces.
type bareTool struct{ name string }

func (t bareTool) Name() string { return t.name }
func (t bareTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return tools.Out{Value: "ran"}, nil
}

// TestNewStepToolForwardsAllCapabilities proves newStepTool forwards
// every optional interface a fully-capable inner implements.
func TestNewStepToolForwardsAllCapabilities(t *testing.T) {
	wrapped := newStepTool("step-full", fullCapTool{name: "inner-full"})

	if wrapped.Name() != "step-full" {
		t.Fatalf("Name = %q, want step-full", wrapped.Name())
	}
	if _, ok := wrapped.(tools.SchemaTool); !ok {
		t.Fatal("wrapped does not implement tools.SchemaTool")
	}
	if _, ok := wrapped.(tools.ProfiledTool); !ok {
		t.Fatal("wrapped does not implement tools.ProfiledTool")
	}
	if _, ok := wrapped.(tools.ResultBudgetTool); !ok {
		t.Fatal("wrapped does not implement tools.ResultBudgetTool")
	}
	if _, ok := wrapped.(tools.PrivilegedTool); !ok {
		t.Fatal("wrapped does not implement tools.PrivilegedTool")
	}

	pt := wrapped.(tools.ProfiledTool)
	if got := pt.ExecutionProfile().Class; got != tools.ExecutionClassWrite {
		t.Fatalf("ExecutionProfile.Class = %q, want write", got)
	}
	bt := wrapped.(tools.ResultBudgetTool)
	if got := bt.MaxResultBytes(); got != 128 {
		t.Fatalf("MaxResultBytes = %d, want 128", got)
	}
	rt := wrapped.(tools.PrivilegedTool)
	if !rt.Privileged() {
		t.Fatal("Privileged() = false, want true")
	}
	st := wrapped.(tools.SchemaTool)
	if string(st.ParameterSchema()) != `{"type":"object"}` {
		t.Fatalf("ParameterSchema = %q", st.ParameterSchema())
	}
	in, err := st.DecodeArguments([]byte("raw"))
	if err != nil {
		t.Fatalf("DecodeArguments: %v", err)
	}
	if in.Value != "raw" {
		t.Fatalf("DecodeArguments Value = %v, want raw", in.Value)
	}

	out, err := wrapped.Run(context.Background(), tools.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Value != "ran" {
		t.Fatalf("Run Value = %v, want ran", out.Value)
	}
}

// The four cap markers below each add exactly the methods of one
// optional interface, with a value distinctive enough that a
// forwarding check cannot pass by accident on a zero value or on
// another capability's value. The sixteen stepInnerN types compose
// them into every subset newStepTool's switch must handle. Bit 0 is
// tools.ProfiledTool, bit 1 is tools.ResultBudgetTool, bit 2 is
// tools.PrivilegedTool, bit 3 is tools.SchemaTool, matching the
// switch's profiled/budgeted/privileged/schemaed order in steptool.go.
type stepProfCapT struct{}

func (stepProfCapT) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfile{Class: tools.ExecutionClassExternal, ResourceKey: "profiled-marker"}
}

type stepBudCapT struct{}

func (stepBudCapT) MaxResultBytes() int { return 4242 }

type stepPrivCapT struct{}

func (stepPrivCapT) Privileged() bool { return true }

type stepSchemaCapT struct{}

func (stepSchemaCapT) ParameterSchema() []byte { return []byte(`{"marker":"schema-marker"}`) }

func (stepSchemaCapT) DecodeArguments(raw []byte) (tools.InOut, error) {
	return tools.InOut{Value: "decoded:" + string(raw)}, nil
}

type stepInner0000 struct{ bareTool }
type stepInner0001 struct {
	bareTool
	stepProfCapT
}
type stepInner0010 struct {
	bareTool
	stepBudCapT
}
type stepInner0011 struct {
	bareTool
	stepProfCapT
	stepBudCapT
}
type stepInner0100 struct {
	bareTool
	stepPrivCapT
}
type stepInner0101 struct {
	bareTool
	stepProfCapT
	stepPrivCapT
}
type stepInner0110 struct {
	bareTool
	stepBudCapT
	stepPrivCapT
}
type stepInner0111 struct {
	bareTool
	stepProfCapT
	stepBudCapT
	stepPrivCapT
}
type stepInner1000 struct {
	bareTool
	stepSchemaCapT
}
type stepInner1001 struct {
	bareTool
	stepProfCapT
	stepSchemaCapT
}
type stepInner1010 struct {
	bareTool
	stepBudCapT
	stepSchemaCapT
}
type stepInner1011 struct {
	bareTool
	stepProfCapT
	stepBudCapT
	stepSchemaCapT
}
type stepInner1100 struct {
	bareTool
	stepPrivCapT
	stepSchemaCapT
}
type stepInner1101 struct {
	bareTool
	stepProfCapT
	stepPrivCapT
	stepSchemaCapT
}
type stepInner1110 struct {
	bareTool
	stepBudCapT
	stepPrivCapT
	stepSchemaCapT
}
type stepInner1111 struct {
	bareTool
	stepProfCapT
	stepBudCapT
	stepPrivCapT
	stepSchemaCapT
}

// stepToolProbes pairs each optional interface with a satisfied check
// on a tools.Tool value. Probe bit i in the subset mask matches probe
// i, mirroring spool_test's TestSpoolToolInterfaceParity fixture.
var stepToolProbes = []struct {
	name    string
	satisfy func(tools.Tool) bool
}{
	{"ProfiledTool", func(t tools.Tool) bool { _, ok := t.(tools.ProfiledTool); return ok }},
	{"ResultBudgetTool", func(t tools.Tool) bool { _, ok := t.(tools.ResultBudgetTool); return ok }},
	{"PrivilegedTool", func(t tools.Tool) bool { _, ok := t.(tools.PrivilegedTool); return ok }},
	{"SchemaTool", func(t tools.Tool) bool { _, ok := t.(tools.SchemaTool); return ok }},
}

// stepInnerFor builds the inner tool for subset mask of
// stepToolProbes.
func stepInnerFor(mask int) tools.Tool {
	inner := bareTool{name: "inner"}
	switch mask {
	case 0:
		return stepInner0000{inner}
	case 1:
		return stepInner0001{inner, stepProfCapT{}}
	case 2:
		return stepInner0010{inner, stepBudCapT{}}
	case 3:
		return stepInner0011{inner, stepProfCapT{}, stepBudCapT{}}
	case 4:
		return stepInner0100{inner, stepPrivCapT{}}
	case 5:
		return stepInner0101{inner, stepProfCapT{}, stepPrivCapT{}}
	case 6:
		return stepInner0110{inner, stepBudCapT{}, stepPrivCapT{}}
	case 7:
		return stepInner0111{inner, stepProfCapT{}, stepBudCapT{}, stepPrivCapT{}}
	case 8:
		return stepInner1000{inner, stepSchemaCapT{}}
	case 9:
		return stepInner1001{inner, stepProfCapT{}, stepSchemaCapT{}}
	case 10:
		return stepInner1010{inner, stepBudCapT{}, stepSchemaCapT{}}
	case 11:
		return stepInner1011{inner, stepProfCapT{}, stepBudCapT{}, stepSchemaCapT{}}
	case 12:
		return stepInner1100{inner, stepPrivCapT{}, stepSchemaCapT{}}
	case 13:
		return stepInner1101{inner, stepProfCapT{}, stepPrivCapT{}, stepSchemaCapT{}}
	case 14:
		return stepInner1110{inner, stepBudCapT{}, stepPrivCapT{}, stepSchemaCapT{}}
	default:
		return stepInner1111{inner, stepProfCapT{}, stepBudCapT{}, stepPrivCapT{}, stepSchemaCapT{}}
	}
}

// TestNewStepToolForwardsPerSubset proves that, for every one of the
// sixteen subsets, each capability the wrapper declares forwards
// inner's own distinctive value rather than a zero value or another
// capability's value. This catches a copy-paste bug where a switch
// branch returns the wrong wrapper variant: a variant built from the
// wrong capability structs would forward the wrong data even though
// TestNewStepToolInterfaceParity's type assertions alone could pass
// for a variant with an accidentally matching interface set.
func TestNewStepToolForwardsPerSubset(t *testing.T) {
	for mask := 0; mask < 1<<len(stepToolProbes); mask++ {
		inner := stepInnerFor(mask)
		wrapped := newStepTool("step", inner)

		if mask&1 != 0 {
			pt, ok := wrapped.(tools.ProfiledTool)
			if !ok {
				t.Fatalf("subset %04b: wrapper does not implement tools.ProfiledTool", mask)
			}
			got := pt.ExecutionProfile()
			want := tools.ExecutionProfile{Class: tools.ExecutionClassExternal, ResourceKey: "profiled-marker"}
			if got != want {
				t.Errorf("subset %04b: ExecutionProfile() = %+v, want %+v", mask, got, want)
			}
		}
		if mask&2 != 0 {
			bt, ok := wrapped.(tools.ResultBudgetTool)
			if !ok {
				t.Fatalf("subset %04b: wrapper does not implement tools.ResultBudgetTool", mask)
			}
			if got := bt.MaxResultBytes(); got != 4242 {
				t.Errorf("subset %04b: MaxResultBytes() = %d, want 4242", mask, got)
			}
		}
		if mask&4 != 0 {
			rt, ok := wrapped.(tools.PrivilegedTool)
			if !ok {
				t.Fatalf("subset %04b: wrapper does not implement tools.PrivilegedTool", mask)
			}
			if !rt.Privileged() {
				t.Errorf("subset %04b: Privileged() = false, want true", mask)
			}
		}
		if mask&8 != 0 {
			st, ok := wrapped.(tools.SchemaTool)
			if !ok {
				t.Fatalf("subset %04b: wrapper does not implement tools.SchemaTool", mask)
			}
			if got := string(st.ParameterSchema()); got != `{"marker":"schema-marker"}` {
				t.Errorf("subset %04b: ParameterSchema() = %s, want schema-marker", mask, got)
			}
			in, err := st.DecodeArguments([]byte("payload"))
			if err != nil {
				t.Errorf("subset %04b: DecodeArguments: %v", mask, err)
			}
			if in.Value != "decoded:payload" {
				t.Errorf("subset %04b: DecodeArguments Value = %v, want decoded:payload", mask, in.Value)
			}
		}

		out, err := wrapped.Run(context.Background(), tools.InOut{})
		if err != nil {
			t.Fatalf("subset %04b: Run: %v", mask, err)
		}
		if out.Value != "ran" {
			t.Errorf("subset %04b: Run Value = %v, want ran", mask, out.Value)
		}
	}
}

// nilSchemaTool implements tools.SchemaTool with a nil schema and a
// recording decode, so the wrapper's forwarding of a nil schema and
// the decode routing into inner are both observable.
type nilSchemaTool struct {
	decoded []byte
}

func (t *nilSchemaTool) Name() string { return "inner-nil-schema" }
func (t *nilSchemaTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return tools.Out{Value: "ran"}, nil
}
func (t *nilSchemaTool) ParameterSchema() []byte { return nil }
func (t *nilSchemaTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	t.decoded = raw
	return tools.InOut{Value: "inner-decoded"}, nil
}

// errDecodeTool implements tools.SchemaTool and always fails decode
// with a sentinel error.
var errDecodeSentinel = errors.New("inner decode sentinel")

type errDecodeTool struct{ name string }

func (t errDecodeTool) Name() string { return t.name }
func (t errDecodeTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return tools.Out{Value: "ran"}, nil
}
func (t errDecodeTool) ParameterSchema() []byte { return []byte(`{"marker":"schema-marker"}`) }
func (t errDecodeTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	return tools.InOut{}, errDecodeSentinel
}

// TestNewStepToolDeclaresAllCapsAlways proves the wrapper declares all
// four optional interfaces for every one of the sixteen inner
// capability subsets, and forwards each capability's value from inner
// through the tools helpers. Rows whose inner lacks a capability pin
// the degraded default: a nil schema and an identity decode for the
// schema-less shape. The interface-presence assertions separate this
// shape from the old exact-parity variants; mask 0000 fails hardest
// against the old code.
func TestNewStepToolDeclaresAllCapsAlways(t *testing.T) {
	for mask := 0; mask < 1<<len(stepToolProbes); mask++ {
		inner := stepInnerFor(mask)
		wrapped := newStepTool("step", inner)

		for _, p := range stepToolProbes {
			if !p.satisfy(wrapped) {
				t.Errorf("subset %04b: wrapper does not implement %s, want it always declared", mask, p.name)
			}
		}

		pt := wrapped.(tools.ProfiledTool)
		if got, want := pt.ExecutionProfile(), tools.ExecutionProfileOf(inner); got != want {
			t.Errorf("subset %04b: ExecutionProfile() = %+v, want inner's %+v", mask, got, want)
		}
		bt := wrapped.(tools.ResultBudgetTool)
		gotN := bt.MaxResultBytes()
		wantN, _ := tools.ResultBudgetOf(inner)
		if gotN != wantN {
			t.Errorf("subset %04b: MaxResultBytes() = %d, want inner's %d", mask, gotN, wantN)
		}
		rt := wrapped.(tools.PrivilegedTool)
		if got, want := rt.Privileged(), tools.IsPrivileged(inner); got != want {
			t.Errorf("subset %04b: Privileged() = %v, want inner's %v", mask, got, want)
		}

		st := wrapped.(tools.SchemaTool)
		gotSchema := st.ParameterSchema()
		wantSchema, _ := tools.SchemaOf(inner)
		if string(gotSchema) != string(wantSchema) {
			t.Errorf("subset %04b: ParameterSchema() = %s, want inner's %s", mask, gotSchema, wantSchema)
		}

		if mask&8 == 0 {
			in, err := st.DecodeArguments([]byte("raw"))
			if err != nil {
				t.Errorf("subset %04b: identity DecodeArguments: %v", mask, err)
			}
			if in.Value != "raw" {
				t.Errorf("subset %04b: identity DecodeArguments Value = %v, want raw", mask, in.Value)
			}
		} else {
			in, err := st.DecodeArguments([]byte("payload"))
			if err != nil {
				t.Errorf("subset %04b: DecodeArguments: %v", mask, err)
			}
			innerST, _ := inner.(tools.SchemaTool)
			want, werr := innerST.DecodeArguments([]byte("payload"))
			if werr != nil {
				t.Fatalf("subset %04b: inner decode: %v", mask, werr)
			}
			if in.Value != want.Value {
				t.Errorf("subset %04b: DecodeArguments Value = %v, want inner's %v", mask, in.Value, want.Value)
			}
		}

		out, err := wrapped.Run(context.Background(), tools.InOut{})
		if err != nil {
			t.Fatalf("subset %04b: Run: %v", mask, err)
		}
		if out.Value != "ran" {
			t.Errorf("subset %04b: Run Value = %v, want ran", mask, out.Value)
		}
	}
}

// TestNewStepToolDeclaresAllCapsAlwaysNilSchema proves a schema-less
// ParameterSchema forwards as nil and the decode routes into inner,
// not the identity path.
func TestNewStepToolDeclaresAllCapsAlwaysNilSchema(t *testing.T) {
	inner := &nilSchemaTool{}
	wrapped := newStepTool("step", inner)

	st, ok := wrapped.(tools.SchemaTool)
	if !ok {
		t.Fatal("wrapper does not implement tools.SchemaTool")
	}
	if got := st.ParameterSchema(); got != nil {
		t.Errorf("ParameterSchema() = %s, want nil", got)
	}
	in, err := st.DecodeArguments([]byte("payload"))
	if err != nil {
		t.Fatalf("DecodeArguments: %v", err)
	}
	if in.Value != "inner-decoded" {
		t.Errorf("DecodeArguments Value = %v, want inner-decoded", in.Value)
	}
	if string(inner.decoded) != "payload" {
		t.Errorf("inner decoded %q, want payload", inner.decoded)
	}
}

// TestNewStepToolDeclaresAllCapsAlwaysDecodeError proves a decode
// failure from inner propagates unwrapped.
func TestNewStepToolDeclaresAllCapsAlwaysDecodeError(t *testing.T) {
	wrapped := newStepTool("step", errDecodeTool{name: "inner-err"})

	st, ok := wrapped.(tools.SchemaTool)
	if !ok {
		t.Fatal("wrapper does not implement tools.SchemaTool")
	}
	_, err := st.DecodeArguments([]byte("payload"))
	if err != errDecodeSentinel {
		t.Fatalf("DecodeArguments error = %v, want the sentinel unwrapped", err)
	}
}

// TestNewStepToolDeclaresAllCapsAlwaysEmptyIdentity proves empty raw
// bytes take the identity path to an empty string value with no error.
func TestNewStepToolDeclaresAllCapsAlwaysEmptyIdentity(t *testing.T) {
	wrapped := newStepTool("step", bareTool{name: "inner-bare"})

	st, ok := wrapped.(tools.SchemaTool)
	if !ok {
		t.Fatal("wrapper does not implement tools.SchemaTool")
	}
	in, err := st.DecodeArguments(nil)
	if err != nil {
		t.Fatalf("DecodeArguments: %v", err)
	}
	s, ok := in.Value.(string)
	if !ok {
		t.Fatalf("DecodeArguments Value type = %T, want string", in.Value)
	}
	if s != "" {
		t.Errorf("DecodeArguments Value = %q, want empty", s)
	}
}
