package machine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// wireRegistry builds the named guards and actions the TDD cases use.
// nil_guard is deliberately bound to a nil func; Decode must reject it.
func wireRegistry() machine.Registry {
	reg := machine.NewRegistry()
	reg.Guards["is_ready"] = busyReady
	reg.Guards["nil_guard"] = nil
	reg.Actions["mark_started"] = busyStart
	reg.Actions["nil_action"] = nil
	return reg
}

// busyReady is a named guard that always passes.
func busyReady(_ context.Context) (bool, error) { return true, nil }

// busyStart is a named entry action that writes the output record.
func busyStart(_ context.Context, rec *machine.InOut) error {
	rec.Output = "started"
	return nil
}

// busyExit is a named exit action with no side effect.
func busyExit(_ context.Context, _ *machine.InOut) error { return nil }

// wireCase holds one Decode case.
type wireCase struct {
	name    string
	json    string
	wantErr bool
	errSub  string
}

// decodeCases lists the Decode assertions for the wire form.
var decodeCases = []wireCase{
	{
		name:    "decodes named guard and action",
		json:    `{"initial":"idle","transitions":[{"from":"idle","to":"running","trigger":"start","guard":"is_ready","on_entry":"mark_started"}]}`,
		wantErr: false,
	},
	{
		name:    "decodes minimal table without names",
		json:    `{"initial":"idle","transitions":[{"from":"idle","to":"running","trigger":"start"}]}`,
		wantErr: false,
	},
	{
		name:    "rejects unknown guard name",
		json:    `{"initial":"idle","transitions":[{"from":"idle","to":"running","trigger":"start","guard":"nope"}]}`,
		wantErr: true,
		errSub:  "not registered",
	},
	{
		name:    "rejects unknown action name",
		json:    `{"initial":"idle","transitions":[{"from":"idle","to":"running","trigger":"start","on_exit":"nope"}]}`,
		wantErr: true,
		errSub:  "not registered",
	},
	{
		name:    "rejects guard name bound to nil func",
		json:    `{"initial":"idle","transitions":[{"from":"idle","to":"running","trigger":"start","guard":"nil_guard"}]}`,
		wantErr: true,
		errSub:  "resolves to nil",
	},
	{
		name:    "rejects action name bound to nil func",
		json:    `{"initial":"idle","transitions":[{"from":"idle","to":"running","trigger":"start","on_entry":"nil_action"}]}`,
		wantErr: true,
		errSub:  "resolves to nil",
	},
	{
		name:    "rejects empty guard name",
		json:    `{"initial":"idle","transitions":[{"from":"idle","to":"running","trigger":"start","guard":""}]}`,
		wantErr: true,
		errSub:  "must not be empty",
	},
	{
		name:    "rejects empty action name",
		json:    `{"initial":"idle","transitions":[{"from":"idle","to":"running","trigger":"start","on_exit":""}]}`,
		wantErr: true,
		errSub:  "must not be empty",
	},
	{
		name:    "rejects self loop",
		json:    `{"initial":"idle","transitions":[{"from":"idle","to":"idle","trigger":"start"}]}`,
		wantErr: true,
		errSub:  "self loop",
	},
	{
		name:    "rejects empty transition list",
		json:    `{"initial":"idle","transitions":[]}`,
		wantErr: true,
		errSub:  "must not be empty",
	},
	{
		name:    "rejects malformed json",
		json:    `{not json`,
		wantErr: true,
		errSub:  "decode",
	},
}

// TestDecodeTable drives the Decode wire cases.
func TestDecodeTable(t *testing.T) {
	t.Parallel()
	reg := wireRegistry()
	for _, tt := range decodeCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := machine.Decode([]byte(tt.json), reg)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errSub) {
					t.Fatalf("error %q should contain %q", err.Error(), tt.errSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestDecodeBindsGuards proves Decode rebinds the named guard and action.
func TestDecodeBindsGuards(t *testing.T) {
	t.Parallel()
	reg := wireRegistry()
	d, err := machine.Decode(
		[]byte(`{"initial":"idle","transitions":[{"from":"idle","to":"running","trigger":"start","guard":"is_ready","on_entry":"mark_started"}]}`),
		reg,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ts := d.Transitions()
	if len(ts) != 1 {
		t.Fatalf("len(Transitions()) = %d, want 1", len(ts))
	}
	if ts[0].Guard == nil {
		t.Fatal("Guard is nil; Decode must bind the named guard")
	}
	if ts[0].OnEntry == nil {
		t.Fatal("OnEntry is nil; Decode must bind the named action")
	}
	// Red step: Decode did not exist on the empty implementation, so this
	// case did not compile. Decode added; the binding cases passed.
}

// encodeCases lists the valid Encode byte assertions for the wire form.
var encodeCases = []struct {
	name string
	json string
}{
	{
		name: "emits named guard and action",
		json: `{"initial":"idle","transitions":[{"from":"idle","to":"running","trigger":"start","guard":"is_ready","on_entry":"mark_started"}]}`,
	},
	{
		name: "emits minimal table without names",
		json: `{"initial":"idle","transitions":[{"from":"idle","to":"running","trigger":"start"}]}`,
	},
}

// TestEncodeRoundTrip proves Encode reproduces the wire names Decode read.
func TestEncodeRoundTrip(t *testing.T) {
	t.Parallel()
	reg := wireRegistry()
	for _, tt := range encodeCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d, err := machine.Decode([]byte(tt.json), reg)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			got, err := d.Encode(reg)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if string(got) != tt.json {
				t.Fatalf("Encode = %s, want %s", got, tt.json)
			}
			// Red step: Encode did not exist on the empty implementation,
			// so this case did not compile. Encode added; bytes matched.
		})
	}
}

// TestEncodeRejectsAnonymousFunc proves a New-built function cannot encode.
func TestEncodeRejectsAnonymousFunc(t *testing.T) {
	t.Parallel()
	d, err := machine.New(
		"idle",
		machine.Transition{
			From:    "idle",
			To:      "running",
			Trigger: "start",
			Guard:   busyReady,
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = d.Encode(machine.NewRegistry())
	if err == nil {
		t.Fatal("expected error for anonymous guard, got nil")
	}
	if !strings.Contains(err.Error(), "no wire name") {
		t.Fatalf("error %q should mention no wire name", err.Error())
	}
	// Red step: Encode did not exist. Before the anonymous-function
	// guard was added, it emitted no name and dropped the guard.
}

// TestEncodeRejectsUnregisteredName proves a decoded name that no longer
// resolves on encode returns an error. The round trip needs the same
// registry on both sides.
func TestEncodeRejectsUnregisteredName(t *testing.T) {
	t.Parallel()
	reg := wireRegistry()
	d, err := machine.Decode(
		[]byte(`{"initial":"idle","transitions":[{"from":"idle","to":"running","trigger":"start","guard":"is_ready"}]}`),
		reg,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = d.Encode(machine.NewRegistry())
	if err == nil {
		t.Fatal("expected error for unregistered name, got nil")
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("error %q should mention not registered", err.Error())
	}
}

// TestEncodeRejectsNilValueRegistry proves a name that maps to a nil func
// on encode returns an error, mirroring the Decode-side nil rejection.
// Encode must not emit bytes that the same registry cannot decode back.
func TestEncodeRejectsNilValueRegistry(t *testing.T) {
	t.Parallel()
	decReg := machine.NewRegistry()
	decReg.Guards["is_ready"] = busyReady
	d, err := machine.Decode(
		[]byte(`{"initial":"idle","transitions":[{"from":"idle","to":"running","trigger":"start","guard":"is_ready"}]}`),
		decReg,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	encReg := machine.NewRegistry()
	encReg.Guards["is_ready"] = nil
	_, err = d.Encode(encReg)
	if err == nil {
		t.Fatal("expected error for name resolving to nil, got nil")
	}
	if !strings.Contains(err.Error(), "resolves to nil") {
		t.Fatalf("error %q should mention resolves to nil", err.Error())
	}
}

// TestEncodeNewStructureOnly proves a New-built table with no bound
// functions survives Encode and a second Decode unchanged.
func TestEncodeNewStructureOnly(t *testing.T) {
	t.Parallel()
	d, err := machine.New(
		"idle",
		machine.Transition{From: "idle", To: "running", Trigger: "start"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := d.Encode(machine.NewRegistry())
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	d2, err := machine.Decode(data, machine.NewRegistry())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	ts := d2.Transitions()
	if len(ts) != 1 || ts[0].From != "idle" || ts[0].To != "running" || ts[0].Trigger != "start" {
		t.Fatalf("round trip table = %#v, want idle->running on start", ts)
	}
}

// TestEncodeRejectsUnregisteredActionName proves Encode fails when an
// on_exit or on_entry name no longer resolves in the encode registry.
// The guard-name path is covered elsewhere; this pins the action paths.
func TestEncodeRejectsUnregisteredActionName(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"on_exit", "on_entry"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			reg := wireRegistry()
			wire := `{"initial":"idle","transitions":[{"from":"idle","to":"running","trigger":"start","` + field + `":"mark_started"}]}`
			d, err := machine.Decode([]byte(wire), reg)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			_, err = d.Encode(machine.NewRegistry())
			if err == nil {
				t.Fatalf("Encode with unregistered %s must fail", field)
			}
			if !strings.Contains(err.Error(), "not registered") {
				t.Fatalf("error %q should mention not registered", err.Error())
			}
		})
	}
}

// TestEncodeRejectsEmptyDefinition proves Encode fails on a zero-value
// Definition, which Validate rejects for having no transitions.
func TestEncodeRejectsEmptyDefinition(t *testing.T) {
	t.Parallel()
	var d machine.Definition
	if _, err := d.Encode(machine.NewRegistry()); err == nil {
		t.Fatal("Encode of a zero-value Definition must fail")
	}
}

// TestNewRegistryEmpty proves NewRegistry starts with empty maps.
func TestNewRegistryEmpty(t *testing.T) {
	t.Parallel()
	reg := machine.NewRegistry()
	if len(reg.Actions) != 0 {
		t.Fatalf("len(Actions) = %d, want 0", len(reg.Actions))
	}
	if len(reg.Guards) != 0 {
		t.Fatalf("len(Guards) = %d, want 0", len(reg.Guards))
	}
	// Red step: Registry and NewRegistry did not exist on the empty
	// implementation, so this case did not compile.
}
