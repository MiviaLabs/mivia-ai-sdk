package machine_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestMachineConformanceVectors pins the machine wire contract. Files in
// machine/testdata/vectors: valid_* must decode (and re-encode to the
// same bytes); invalid_decode_* must fail Decode. Add a vector whenever
// the schema or a rule changes.
func TestMachineConformanceVectors(t *testing.T) {
	reg := machine.NewRegistry()
	reg.Guards["is_ready"] = busyReady
	reg.Guards["nil_handler"] = nil
	reg.Actions["mark_started"] = busyStart
	reg.Actions["mark_left"] = busyExit

	entries, err := os.ReadDir("../testdata/vectors")
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("../testdata/vectors", name))
			if err != nil {
				t.Fatalf("read vector: %v", err)
			}
			d, decErr := machine.Decode(data, reg)
			switch {
			case strings.HasPrefix(name, "valid_"):
				if decErr != nil {
					t.Fatalf("valid vector rejected: %v", decErr)
				}
				back, err := d.Encode(reg)
				if err != nil {
					t.Fatalf("valid vector did not re-encode: %v", err)
				}
				if string(back) != strings.TrimRight(string(data), "\n") {
					t.Fatalf("re-encode changed the vector json:\n%s\nwant\n%s", back, data)
				}
			case strings.HasPrefix(name, "invalid_decode_"):
				if decErr == nil {
					t.Fatal("invalid vector decoded without error")
				}
			default:
				t.Fatal("vector name must start with valid_ or invalid_decode_")
			}
		})
	}
}
