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
	defs, err := agentloop.Definitions(tools.New(), nil)
	if err != nil {
		t.Fatalf("Definitions() error = %v, want nil", err)
	}
	if len(defs) != 0 {
		t.Fatalf("Definitions() = %v, want empty", defs)
	}
}

// TestDefinitionsSkipsSchemaFreeTools proves a mixed registry fails
// loudly: a schema-free registered tool fails Definitions with
// ErrNoSchema naming it, instead of a silent skip.
func TestDefinitionsSkipsSchemaFreeTools(t *testing.T) {
	reg := tools.New()
	mustAdd(t, reg, &schemaEchoTool{name: "with-schema", schema: []byte(`{}`)})
	mustAdd(t, reg, &noSchemaTool{name: "without-schema"})

	defs, err := agentloop.Definitions(reg, nil)
	if !errors.Is(err, agentloop.ErrNoSchema) {
		t.Fatalf("Definitions() error = %v, want ErrNoSchema", err)
	}
	if defs != nil {
		t.Fatalf("Definitions() defs = %v, want nil on failure", defs)
	}
}

// TestDefinitionsSkipsNilSchemaSchemaTool proves a tool implementing
// tools.SchemaTool whose ParameterSchema returns nil fails Definitions
// with ErrNoSchema: SchemaOf fails closed on nil schema bytes, so the
// wrapper's name appears in the wrapped error.
func TestDefinitionsSkipsNilSchemaSchemaTool(t *testing.T) {
	reg := tools.New()
	mustAdd(t, reg, &schemaEchoTool{name: "nil-schema", schema: nil})

	defs, err := agentloop.Definitions(reg, nil)
	if !errors.Is(err, agentloop.ErrNoSchema) {
		t.Fatalf("Definitions() error = %v, want ErrNoSchema", err)
	}
	if len(defs) != 0 {
		t.Fatalf("Definitions() defs = %v, want none: a nil schema is not a published schema", defs)
	}
	if !strings.Contains(err.Error(), "nil-schema") {
		t.Fatalf("err = %v, want it to name the schema-free tool", err)
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

	defs, err := agentloop.Definitions(reg, scope)
	if err != nil {
		t.Fatalf("Definitions() error = %v, want nil", err)
	}
	if len(defs) != 1 || defs[0].Name != "allowed" {
		t.Fatalf("Definitions() defs = %v, want one entry named allowed", defs)
	}
}

// TestDefinitionsErrNoSchemasEveryToolMissingSchema proves a registry
// whose every tool lacks a schema fails loudly with ErrNoSchema naming
// the first tool in sorted order; the empty-set cause is unreachable
// past ErrNoSchema.
func TestDefinitionsErrNoSchemasEveryToolMissingSchema(t *testing.T) {
	reg := tools.New()
	mustAdd(t, reg, &noSchemaTool{name: "a"})
	mustAdd(t, reg, &noSchemaTool{name: "b"})

	defs, err := agentloop.Definitions(reg, nil)
	if !errors.Is(err, agentloop.ErrNoSchema) {
		t.Fatalf("Definitions() error = %v, want ErrNoSchema", err)
	}
	if len(defs) != 0 {
		t.Fatalf("Definitions() defs = %v, want none", defs)
	}
	if !strings.Contains(err.Error(), "a") {
		t.Fatalf("err = %v, want it to name the first tool in sorted order", err)
	}
}

// TestDefinitionsRejectsSchemaFreeToolNames proves the loud failure's
// shape on a mixed registry: a wrapped ErrNoSchema whose message names
// the schema-free tool, and a nil definition set.
func TestDefinitionsRejectsSchemaFreeToolNames(t *testing.T) {
	reg := tools.New()
	mustAdd(t, reg, &schemaEchoTool{name: "good", schema: []byte(`{}`)})
	mustAdd(t, reg, &noSchemaTool{name: "schema-free"})

	defs, err := agentloop.Definitions(reg, nil)
	if !errors.Is(err, agentloop.ErrNoSchema) {
		t.Fatalf("Definitions() error = %v, want ErrNoSchema", err)
	}
	if !strings.Contains(err.Error(), "schema-free") {
		t.Fatalf("err = %v, want it to name the schema-free tool", err)
	}
	if defs != nil {
		t.Fatalf("Definitions() defs = %v, want nil on failure", defs)
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

	defs, err := agentloop.Definitions(reg, scope)
	if !errors.Is(err, agentloop.ErrNoSchemas) {
		t.Fatalf("Definitions() error = %v, want ErrNoSchemas", err)
	}
	if len(defs) != 0 {
		t.Fatalf("Definitions() defs = %v, want none", defs)
	}
}

// TestDefinitionsSkipsScopeDeniedSchemaFreeTool proves a scope-denied
// tool is skipped before its schema is ever read: a schema-free tool
// the scope excludes does not fail Definitions, since denial removes
// it from the walk before the ErrNoSchema check runs.
func TestDefinitionsSkipsScopeDeniedSchemaFreeTool(t *testing.T) {
	reg := tools.New()
	mustAdd(t, reg, &schemaEchoTool{name: "allowed", schema: []byte(`{}`)})
	mustAdd(t, reg, &noSchemaTool{name: "denied-no-schema"})
	scope := tools.NewScope(tools.ScopeOptions{Allowlist: []string{"allowed"}})

	defs, err := agentloop.Definitions(reg, scope)
	if err != nil {
		t.Fatalf("Definitions() error = %v, want nil", err)
	}
	if len(defs) != 1 || defs[0].Name != "allowed" {
		t.Fatalf("Definitions() defs = %v, want one entry named allowed", defs)
	}
}

// TestNewSucceedsWithScopeDeniedSchemaFreeTool proves New itself
// agrees with Definitions: a schema-free tool the Scope excludes never
// reaches the schema check, so New builds the Loop.
func TestNewSucceedsWithScopeDeniedSchemaFreeTool(t *testing.T) {
	reg := tools.New()
	mustAdd(t, reg, &schemaEchoTool{name: "allowed", schema: []byte(`{}`)})
	mustAdd(t, reg, &noSchemaTool{name: "denied-no-schema"})
	scope := tools.NewScope(tools.ScopeOptions{Allowlist: []string{"allowed"}})

	_, err := agentloop.New(agentloop.Options{
		Completer: &scriptedCompleter{},
		Tools:     reg,
		Scope:     scope,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
}

// mustAdd registers t onto reg, failing the test on error.
func mustAdd(t *testing.T, reg *tools.Registry, tool tools.Tool) {
	t.Helper()
	if err := reg.Add(tool); err != nil {
		t.Fatalf("Add(%s) error = %v, want nil", tool.Name(), err)
	}
}
