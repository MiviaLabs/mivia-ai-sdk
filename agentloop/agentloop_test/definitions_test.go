package agentloop_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestDefinitionsEmptyRegistry proves an empty Registry yields an
// empty, no-error result.
func TestDefinitionsEmptyRegistry(t *testing.T) {
	defs, skipped, err := agentloop.Definitions(tools.New(), nil)
	if err != nil {
		t.Fatalf("Definitions() error = %v, want nil", err)
	}
	if len(defs) != 0 || len(skipped) != 0 {
		t.Fatalf("Definitions() = %v, %v, want both empty", defs, skipped)
	}
}

// TestDefinitionsSkipsSchemaFreeTools proves a schema-bearing tool is
// offered and a schema-free tool is skipped and reported.
func TestDefinitionsSkipsSchemaFreeTools(t *testing.T) {
	reg := tools.New()
	mustAdd(t, reg, &schemaEchoTool{name: "with-schema", schema: []byte(`{}`)})
	mustAdd(t, reg, &noSchemaTool{name: "without-schema"})

	defs, skipped, err := agentloop.Definitions(reg, nil)
	if err != nil {
		t.Fatalf("Definitions() error = %v, want nil", err)
	}
	if len(defs) != 1 || defs[0].Name != "with-schema" {
		t.Fatalf("Definitions() defs = %v, want one entry named with-schema", defs)
	}
	if len(skipped) != 1 || skipped[0] != "without-schema" {
		t.Fatalf("Definitions() skipped = %v, want [without-schema]", skipped)
	}
}

// TestDefinitionsSkipsNilSchemaSchemaTool proves a tool implementing
// tools.SchemaTool whose ParameterSchema returns nil lands in the skip
// list: SchemaOf fails closed on nil schema bytes, so the tool is
// offered in no definition and its name appears in no error. A
// one-tool registry whose offered set ends empty fails closed with
// ErrNoSchemas, per Definitions' documented contract.
func TestDefinitionsSkipsNilSchemaSchemaTool(t *testing.T) {
	reg := tools.New()
	mustAdd(t, reg, &schemaEchoTool{name: "nil-schema", schema: nil})

	defs, skipped, err := agentloop.Definitions(reg, nil)
	if !errors.Is(err, agentloop.ErrNoSchemas) {
		t.Fatalf("Definitions() error = %v, want ErrNoSchemas", err)
	}
	if len(defs) != 0 {
		t.Fatalf("Definitions() defs = %v, want none: a nil schema is not a published schema", defs)
	}
	if len(skipped) != 1 || skipped[0] != "nil-schema" {
		t.Fatalf("Definitions() skipped = %v, want [nil-schema]", skipped)
	}
	if strings.Contains(err.Error(), "nil-schema") {
		t.Fatalf("err = %v, want it to not name the skipped tool", err)
	}
}

// TestDefinitionsScopeDenial proves a Scope denial removes a
// schema-bearing tool from the offered set without adding it to the
// skip list.
func TestDefinitionsScopeDenial(t *testing.T) {
	reg := tools.New()
	mustAdd(t, reg, &schemaEchoTool{name: "allowed", schema: []byte(`{}`)})
	mustAdd(t, reg, &schemaEchoTool{name: "denied", schema: []byte(`{}`)})
	scope := tools.NewScope(tools.ScopeOptions{Allowlist: []string{"allowed"}})

	defs, skipped, err := agentloop.Definitions(reg, scope)
	if err != nil {
		t.Fatalf("Definitions() error = %v, want nil", err)
	}
	if len(defs) != 1 || defs[0].Name != "allowed" {
		t.Fatalf("Definitions() defs = %v, want one entry named allowed", defs)
	}
	if len(skipped) != 0 {
		t.Fatalf("Definitions() skipped = %v, want none: scope denial is not a schema skip", skipped)
	}
}

// TestDefinitionsErrNoSchemasEveryToolMissingSchema proves a registry
// whose every tool lacks a schema fails closed.
func TestDefinitionsErrNoSchemasEveryToolMissingSchema(t *testing.T) {
	reg := tools.New()
	mustAdd(t, reg, &noSchemaTool{name: "a"})
	mustAdd(t, reg, &noSchemaTool{name: "b"})

	defs, skipped, err := agentloop.Definitions(reg, nil)
	if !errors.Is(err, agentloop.ErrNoSchemas) {
		t.Fatalf("Definitions() error = %v, want ErrNoSchemas", err)
	}
	if len(defs) != 0 {
		t.Fatalf("Definitions() defs = %v, want none", defs)
	}
	if len(skipped) != 2 {
		t.Fatalf("Definitions() skipped = %v, want both names", skipped)
	}
}

// TestDefinitionsErrNoSchemasScopeDeniesEveryTool proves the broadened
// fail-closed condition trips even when the skip list stays empty:
// every tool has a schema, but the Scope denies all of them.
func TestDefinitionsErrNoSchemasScopeDeniesEveryTool(t *testing.T) {
	reg := tools.New()
	mustAdd(t, reg, &schemaEchoTool{name: "a", schema: []byte(`{}`)})
	mustAdd(t, reg, &schemaEchoTool{name: "b", schema: []byte(`{}`)})
	scope := tools.NewScope(tools.ScopeOptions{ExtraDenylist: []string{"a", "b"}})

	defs, skipped, err := agentloop.Definitions(reg, scope)
	if !errors.Is(err, agentloop.ErrNoSchemas) {
		t.Fatalf("Definitions() error = %v, want ErrNoSchemas", err)
	}
	if len(defs) != 0 {
		t.Fatalf("Definitions() defs = %v, want none", defs)
	}
	if len(skipped) != 0 {
		t.Fatalf("Definitions() skipped = %v, want none: both tools had schemas", skipped)
	}
}

// mustAdd registers t onto reg, failing the test on error.
func mustAdd(t *testing.T, reg *tools.Registry, tool tools.Tool) {
	t.Helper()
	if err := reg.Add(tool); err != nil {
		t.Fatalf("Add(%s) error = %v, want nil", tool.Name(), err)
	}
}
