package agentloop

import (
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// Definitions builds []provider.ToolDefinition from reg, offering
// only a tool that publishes a schema through tools.SchemaOf and
// passes scope's check when scope is non-nil. The second return holds
// the names skipped for a missing schema, independent of scope. A
// scope-denied tool is left out of both the offered set and the skip
// list. Definitions fails closed: it returns ErrNoSchemas whenever
// reg holds at least one tool and the offered set ends up empty,
// whatever the cause. An empty reg returns an empty set and no error.
func Definitions(reg *tools.Registry, scope *tools.Scope) ([]provider.ToolDefinition, []string, error) {
	all := reg.Tools()
	defs := make([]provider.ToolDefinition, 0, len(all))
	var skipped []string

	for _, t := range all {
		schema, ok := tools.SchemaOf(t)
		if !ok {
			skipped = append(skipped, t.Name())
			continue
		}
		if scope != nil && !scope.Allowed(t.Name(), t) {
			continue
		}
		defs = append(defs, provider.ToolDefinition{Name: t.Name(), Schema: schema})
	}

	if len(all) > 0 && len(defs) == 0 {
		return defs, skipped, ErrNoSchemas
	}
	return defs, skipped, nil
}
