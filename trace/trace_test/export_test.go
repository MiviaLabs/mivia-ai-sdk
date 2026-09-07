package trace_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/trace"
)

// decodedLine is the shape one WriteJSONLines line decodes into.
type decodedLine struct {
	Name       string            `json:"name"`
	Start      string            `json:"start"`
	End        string            `json:"end"`
	Parent     trace.SpanID      `json:"parent"`
	Attributes map[string]string `json:"attributes"`
}

// TestWriteJSONLinesEmptySlice proves an empty spans slice writes
// nothing to w.
func TestWriteJSONLinesEmptySlice(t *testing.T) {
	var buf bytes.Buffer
	if err := trace.WriteJSONLines(&buf, nil); err != nil {
		t.Fatalf("WriteJSONLines() error = %v, want nil", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("WriteJSONLines() wrote %q, want empty", buf.String())
	}
}

// TestWriteJSONLinesNoParent proves a root span, with no parent,
// encodes a zero parent field.
func TestWriteJSONLinesNoParent(t *testing.T) {
	tr := trace.New()
	_, root := tr.Start(context.Background(), "root")
	root.SetAttribute("key", "value")
	root.End()

	var buf bytes.Buffer
	if err := trace.WriteJSONLines(&buf, tr.Spans()); err != nil {
		t.Fatalf("WriteJSONLines() error = %v, want nil", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("line count = %d, want 1", len(lines))
	}

	var got decodedLine
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v", lines[0], err)
	}
	if got.Name != "root" {
		t.Errorf("Name = %q, want %q", got.Name, "root")
	}
	if got.Parent != 0 {
		t.Errorf("Parent = %d, want 0", got.Parent)
	}
	if got.Attributes["key"] != "value" {
		t.Errorf("Attributes[%q] = %q, want %q", "key", got.Attributes["key"], "value")
	}
}

// TestWriteJSONLinesParentChild proves a child span's parent field
// carries the root's ID, and one line is written per span in order.
func TestWriteJSONLinesParentChild(t *testing.T) {
	tr := trace.New()
	rootCtx, root := tr.Start(context.Background(), "root")
	_, child := tr.Start(rootCtx, "child")
	root.End()
	child.End()

	var buf bytes.Buffer
	if err := trace.WriteJSONLines(&buf, tr.Spans()); err != nil {
		t.Fatalf("WriteJSONLines() error = %v, want nil", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("line count = %d, want 2", len(lines))
	}

	var second decodedLine
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v", lines[1], err)
	}
	if second.Name != "child" {
		t.Errorf("Name = %q, want %q", second.Name, "child")
	}
	if second.Parent != root.ID {
		t.Errorf("Parent = %d, want %d", second.Parent, root.ID)
	}
}
