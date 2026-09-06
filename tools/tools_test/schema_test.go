package tools_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// schemaTool implements tools.SchemaTool for TestSchemaOf.
type schemaTool struct {
	stubTool
	schema []byte
}

// ParameterSchema is nil-receiver safe so TestSchemaOf's typed-nil
// case exercises SchemaOf's type assertion without panicking.
func (s *schemaTool) ParameterSchema() []byte {
	if s == nil {
		return nil
	}
	return s.schema
}

func (s *schemaTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	return tools.InOut{Value: string(raw)}, nil
}

// TestSchemaOf covers the SchemaTool-implementing case, the
// non-implementing case, a non-nil receiver publishing no schema
// bytes, and a typed nil.
func TestSchemaOf(t *testing.T) {
	t.Run("implements SchemaTool", func(t *testing.T) {
		want := []byte(`{"type":"object"}`)
		st := &schemaTool{stubTool: stubTool{name: "schema"}, schema: want}
		got, ok := tools.SchemaOf(st)
		if !ok {
			t.Fatalf("SchemaOf ok = false, want true")
		}
		if string(got) != string(want) {
			t.Fatalf("SchemaOf = %s, want %s", got, want)
		}
	})
	t.Run("does not implement SchemaTool", func(t *testing.T) {
		got, ok := tools.SchemaOf(&stubTool{name: "plain"})
		if ok {
			t.Fatalf("SchemaOf ok = true, want false")
		}
		if got != nil {
			t.Fatalf("SchemaOf = %v, want nil", got)
		}
	})
	t.Run("nil schema fails closed", func(t *testing.T) {
		st := &schemaTool{stubTool: stubTool{name: "schemaless"}}
		got, ok := tools.SchemaOf(st)
		if ok {
			t.Fatalf("SchemaOf(nil schema) ok = true, want false: a nil schema is not a published schema")
		}
		if got != nil {
			t.Fatalf("SchemaOf(nil schema) = %v, want nil", got)
		}
	})
	t.Run("typed nil", func(t *testing.T) {
		var st *schemaTool
		got, ok := tools.SchemaOf(st)
		// Fail closed: a typed nil implements SchemaTool, but its
		// ParameterSchema returns nil, so SchemaOf reports nil, false.
		// A nil schema must never read as published.
		if ok {
			t.Fatalf("SchemaOf(typed nil) ok = true, want false: nil schema bytes fail closed")
		}
		if got != nil {
			t.Fatalf("SchemaOf(typed nil) = %v, want nil", got)
		}
	})
}

// TestSchemaToolDecodeArguments proves the interface's decode method
// is directly callable, independent of SchemaOf.
func TestSchemaToolDecodeArguments(t *testing.T) {
	st := &schemaTool{stubTool: stubTool{name: "schema"}}
	in, err := st.DecodeArguments([]byte("raw"))
	if err != nil {
		t.Fatalf("DecodeArguments error = %v, want nil", err)
	}
	if in.Value != "raw" {
		t.Fatalf("DecodeArguments.Value = %v, want raw", in.Value)
	}
	_ = context.Background()
}
