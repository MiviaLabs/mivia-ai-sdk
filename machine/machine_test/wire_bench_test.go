package machine_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// buildWireTable builds a decoded ten-row table with named guards.
// Each row has its own status and trigger so the table is unambiguous.
func buildWireTable() (*machine.Definition, machine.Registry) {
	reg := machine.NewRegistry()
	reg.Guards["ready"] = busyReady
	cells := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		cells = append(cells,
			`{"from":"s`+strconv.Itoa(i)+`","to":"s`+strconv.Itoa(i+1)+`","trigger":"t`+strconv.Itoa(i)+`","guard":"ready"}`)
	}
	wire := []byte(`{"initial":"s0","transitions":[` + strings.Join(cells, ",") + `]}`)
	d, err := machine.Decode(wire, reg)
	if err != nil {
		panic("buildWireTable Decode: " + err.Error())
	}
	return &d, reg
}

// BenchmarkWireRoundTripTen benchmarks Decode + Encode on a ten-row table.
// Target: under two microseconds.
// Baseline (empty implementation): no benchmark; Encode and Decode were absent.
// Measured with implementation: ~8.2 us/op, ~85 allocs/op. The cost is
// the JSON parse and serialize of a full ten-row table; encoding/json
// dominates the round trip on this Ryzen 9 box.
func BenchmarkWireRoundTripTen(b *testing.B) {
	d, reg := buildWireTable()
	wire := mustEncode(b, d, reg)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := machine.Decode(wire, reg)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := r.Encode(reg); err != nil {
			b.Fatal(err)
		}
	}
}

// mustEncode returns the wire bytes for d, or fails the benchmark.
func mustEncode(b *testing.B, d *machine.Definition, reg machine.Registry) []byte {
	b.Helper()
	data, err := d.Encode(reg)
	if err != nil {
		b.Fatal(err)
	}
	return data
}
