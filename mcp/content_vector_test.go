package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// vectorContentBlock is the expected shape of one mapped ContentBlock
// in a conformance vector.
type vectorContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     []byte `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// vector is one conformance vector under testdata/vectors: Result is
// the wire shape of an mcpsdk.CallToolResult; Expected is the
// CallResult this package's mapping must produce from it.
type vector struct {
	Result   json.RawMessage `json:"result"`
	Expected struct {
		Content []vectorContentBlock `json:"content"`
		IsError bool                 `json:"isError"`
	} `json:"expected"`
}

// TestConformanceVectors decodes every valid_*.json vector's Result
// through mcpsdk.CallToolResult's own UnmarshalJSON method, maps it
// through mapCallResult, and compares the outcome against the
// vector's Expected shape.
func TestConformanceVectors(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join("testdata", "vectors", "valid_*.json"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no valid_*.json vectors found")
	}
	for _, path := range matches {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			var v vector
			if err := json.Unmarshal(raw, &v); err != nil {
				t.Fatalf("Unmarshal vector: %v", err)
			}

			var wire mcpsdk.CallToolResult
			if err := json.Unmarshal(v.Result, &wire); err != nil {
				t.Fatalf("Unmarshal result through mcpsdk.CallToolResult: %v", err)
			}

			got, err := mapCallResult(&wire)
			if err != nil {
				t.Fatalf("mapCallResult: %v", err)
			}

			if got.IsError != v.Expected.IsError {
				t.Fatalf("IsError = %v, want %v", got.IsError, v.Expected.IsError)
			}
			if len(got.Content) != len(v.Expected.Content) {
				t.Fatalf("len(Content) = %d, want %d", len(got.Content), len(v.Expected.Content))
			}
			for i, want := range v.Expected.Content {
				block := got.Content[i]
				if block.Type != want.Type {
					t.Fatalf("Content[%d].Type = %q, want %q", i, block.Type, want.Type)
				}
				if block.Text != want.Text {
					t.Fatalf("Content[%d].Text = %q, want %q", i, block.Text, want.Text)
				}
				if string(block.Data) != string(want.Data) {
					t.Fatalf("Content[%d].Data = %q, want %q", i, block.Data, want.Data)
				}
				if block.MimeType != want.MimeType {
					t.Fatalf("Content[%d].MimeType = %q, want %q", i, block.MimeType, want.MimeType)
				}
			}
		})
	}
}
