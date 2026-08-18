package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// FuzzMapCallResult feeds arbitrary bytes to mcpsdk.CallToolResult's
// own UnmarshalJSON, the same decode path the conformance vectors in
// TestConformanceVectors use, then to mapCallResult. Anything the SDK
// accepts must map without a panic: mapCallResult may return an error,
// but it must never crash on a value the SDK itself considers valid
// wire content. Run: go test -fuzz=FuzzMapCallResult ./mcp
func FuzzMapCallResult(f *testing.F) {
	matches, err := filepath.Glob(filepath.Join("testdata", "vectors", "valid_*.json"))
	if err != nil {
		f.Fatalf("Glob: %v", err)
	}
	for _, path := range matches {
		raw, err := os.ReadFile(path)
		if err != nil {
			f.Fatalf("ReadFile: %v", err)
		}
		var v vector
		if err := json.Unmarshal(raw, &v); err != nil {
			f.Fatalf("Unmarshal vector: %v", err)
		}
		f.Add([]byte(v.Result))
	}
	f.Add([]byte{})
	f.Add([]byte("{}"))
	f.Add([]byte(`{"content":[{"type":"text"`))
	f.Add([]byte(`{"content":[{"type":"unknown_block_kind"}]}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var wire mcpsdk.CallToolResult
		if err := json.Unmarshal(data, &wire); err != nil {
			return
		}
		got, err := mapCallResult(&wire)
		if err != nil {
			return
		}
		if got == nil {
			t.Fatal("mapCallResult returned a nil *CallResult with a nil error")
		}
		if len(got.Content) != len(wire.Content) {
			t.Fatalf("len(got.Content) = %d, want %d (one mapped block per decoded block)", len(got.Content), len(wire.Content))
		}
	})
}
