package flow_test

// Fuzz: flow.Decode takes arbitrary wire bytes, mirroring
// machine.Decode's FuzzDecode (machine/machine_test/fuzz_test.go) and
// envelope.Decode's FuzzDecode (envelope/fuzz_test.go).

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
)

// FuzzCheckpointDecode feeds arbitrary bytes to flow.Decode. It must
// never panic, and anything it accepts must re-encode cleanly.
// Run: go test -fuzz=FuzzCheckpointDecode ./flow/flow_test/
func FuzzCheckpointDecode(f *testing.F) {
	valid := flow.Checkpoint{Status: statusDone, Done: []string{"a", "b"}}
	seed, err := valid.Encode()
	if err != nil {
		f.Fatalf("Encode seed: %v", err)
	}
	f.Add(seed)
	f.Add([]byte{})
	f.Add([]byte("{}"))
	f.Add([]byte(`{"Status":"done"`))
	f.Add([]byte(`{"Status":"","Done":["a"]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := flow.Decode(data)
		if err != nil {
			return
		}
		if _, err := c.Encode(); err != nil {
			t.Fatalf("decoded but cannot re-encode: %v", err)
		}
	})
}
