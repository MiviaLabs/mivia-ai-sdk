package mcp

import (
	"encoding/json"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestListToolsPublishesToolsSchema proves a mapped MCP tool is
// schema-bearing to tools.SchemaOf, so agentloop.Definitions offers it
// instead of skipping it as schemaless.
func TestListToolsPublishesToolsSchema(t *testing.T) {
	server := newFixtureServer(nil)
	addEchoTool(server, "echo")
	c := connectFixture(t, server, ClientOptions{})

	list, err := c.ListTools(t.Context())
	if err != nil {
		t.Fatalf("ListTools() error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListTools() returned %d tools, want 1", len(list))
	}

	schema, ok := tools.SchemaOf(list[0])
	if !ok {
		t.Fatal("tools.SchemaOf() = false, want true: mapped MCP tool must implement tools.SchemaTool")
	}
	if len(schema) == 0 {
		t.Fatal("tools.SchemaOf() returned empty schema bytes")
	}
	var decoded map[string]any
	if err := json.Unmarshal(schema, &decoded); err != nil {
		t.Fatalf("schema is not JSON: %v, got %s", err, schema)
	}
	if decoded["type"] != "object" {
		t.Errorf("schema type = %v, want %q", decoded["type"], "object")
	}
}

// TestDecodeArguments covers the raw-bytes-to-InOut half of
// tools.SchemaTool for a mapped MCP tool.
func TestDecodeArguments(t *testing.T) {
	server := newFixtureServer(nil)
	addEchoTool(server, "echo")
	c := connectFixture(t, server, ClientOptions{})

	list, err := c.ListTools(t.Context())
	if err != nil {
		t.Fatalf("ListTools() error: %v", err)
	}
	st, ok := list[0].(tools.SchemaTool)
	if !ok {
		t.Fatal("mapped tool does not implement tools.SchemaTool")
	}

	t.Run("object arguments", func(t *testing.T) {
		in, err := st.DecodeArguments([]byte(`{"message":"hi"}`))
		if err != nil {
			t.Fatalf("DecodeArguments() error: %v", err)
		}
		args, ok := in.Value.(map[string]any)
		if !ok {
			t.Fatalf("in.Value is %T, want map[string]any", in.Value)
		}
		if args["message"] != "hi" {
			t.Errorf("args[message] = %v, want %q", args["message"], "hi")
		}
	})

	t.Run("empty arguments send none", func(t *testing.T) {
		in, err := st.DecodeArguments(nil)
		if err != nil {
			t.Fatalf("DecodeArguments(nil) error: %v", err)
		}
		if in.Value != nil {
			t.Errorf("in.Value = %v, want nil", in.Value)
		}
	})

	t.Run("malformed arguments fail", func(t *testing.T) {
		if _, err := st.DecodeArguments([]byte("{not json")); err == nil {
			t.Fatal("DecodeArguments(malformed) = nil error, want error")
		}
	})
}

// TestDecodedArgumentsRunEndToEnd proves DecodeArguments produces a
// value Run actually accepts, not merely a well-typed one.
func TestDecodedArgumentsRunEndToEnd(t *testing.T) {
	server := newFixtureServer(nil)
	addEchoTool(server, "echo")
	c := connectFixture(t, server, ClientOptions{})

	list, err := c.ListTools(t.Context())
	if err != nil {
		t.Fatalf("ListTools() error: %v", err)
	}
	in, err := list[0].(tools.SchemaTool).DecodeArguments([]byte(`{"message":"round trip"}`))
	if err != nil {
		t.Fatalf("DecodeArguments() error: %v", err)
	}
	out, err := list[0].Run(t.Context(), in)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	result, ok := out.Value.(*CallResult)
	if !ok {
		t.Fatalf("out.Value is %T, want *CallResult", out.Value)
	}
	if result.IsError {
		t.Fatalf("CallResult.IsError = true, want false")
	}
}
