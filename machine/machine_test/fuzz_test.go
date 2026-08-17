package machine_test

import (
	"context"
	"os"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// fuzzRegistry builds the registrar the valid vectors reference.
// Guards and actions must resolve for a decoded definition to re-encode.
func fuzzRegistry() *machine.Registry {
	reg := machine.NewRegistry()
	reg.Guards["is_ready"] = func(_ context.Context) (bool, error) { return true, nil }
	reg.Actions["mark_started"] = func(_ context.Context, _ *machine.InOut) error { return nil }
	reg.Actions["mark_left"] = func(_ context.Context, _ *machine.InOut) error { return nil }
	return &reg
}

// FuzzDecode feeds arbitrary bytes to Decode. It must never panic, and
// anything it accepts must re-encode cleanly with the same registry.
// Run: go test -fuzz=FuzzDecode ./machine/machine_test/
func FuzzDecode(f *testing.F) {
	reg := fuzzRegistry()
	for _, seed := range []string{
		"../testdata/vectors/valid_basic.json",
		"../testdata/vectors/valid_minimal.json",
	} {
		data, err := os.ReadFile(seed)
		if err != nil {
			f.Fatalf("read seed: %v", err)
		}
		f.Add(data)
	}
	f.Add([]byte{})
	f.Add([]byte("{}"))
	f.Add([]byte(`{"initial":"idle"`))
	f.Add([]byte(`{"initial":"idle","transitions":[{"from":"idle","to":"running","trigger":"start","guard":"is_ready"}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := machine.Decode(data, *reg)
		if err != nil {
			return
		}
		if _, err := d.Encode(*reg); err != nil {
			t.Fatalf("decoded but cannot re-encode: %v", err)
		}
	})
}
