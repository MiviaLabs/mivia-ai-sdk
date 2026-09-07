package agentloop

import (
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// Definitions builds []provider.ToolDefinition from reg, offering
// only a tool that passes scope's check when scope is non-nil. A
// scope-denied tool is skipped before its schema is ever read: denial
// is policy filtering, not a mistake, so a schema-less tool the scope
// excludes never fails Definitions. A scope-allowed tool that
// publishes no parameter schema through tools.SchemaOf fails
// Definitions with ErrNoSchema, wrapped with the tool's registry
// name. Definitions still fails closed with ErrNoSchemas when reg
// holds at least one tool and the offered set ends up empty; past
// ErrNoSchema that means the scope denied every tool. An empty reg
// returns an empty set and no error.
func Definitions(reg *tools.Registry, scope *tools.Scope) ([]provider.ToolDefinition, error) {
	all := reg.Tools()
	defs := make([]provider.ToolDefinition, 0, len(all))

	for _, t := range all {
		if scope != nil && !scope.Allowed(t.Name(), t) {
			continue
		}
		schema, ok := tools.SchemaOf(t)
		if !ok {
			return nil, fmt.Errorf("agentloop: tool %q: %w", t.Name(), ErrNoSchema)
		}
		defs = append(defs, provider.ToolDefinition{Name: t.Name(), Schema: schema})
	}

	if len(all) > 0 && len(defs) == 0 {
		return defs, ErrNoSchemas
	}
	return defs, nil
}
