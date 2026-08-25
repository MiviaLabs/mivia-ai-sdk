package agentloop

import (
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/schema"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// Surface is one iteration's tool surface, produced by
// Options.Surface. Advertised is what the model is offered this
// iteration (compiled into the loop's schemas); Registry is where
// model-chosen calls resolve; Scope optionally narrows Registry.
// Advertised MAY name tools whose definitions are not backed by
// Registry entries and vice versa: the check kit treats the two sets
// as independent, mirroring hosts that advertise a union while
// gating execution through wrapper denial.
type Surface struct {
	// Advertised is the tool-definition list sent to the model this
	// iteration. Replaces the previous iteration's set wholesale.
	Advertised []provider.ToolDefinition
	// Registry resolves model-chosen calls this iteration. Nil keeps
	// the previous iteration's registry.
	Registry *tools.Registry
	// Scope narrows Registry lookups this iteration. Nil keeps the
	// previous iteration's scope.
	Scope *tools.Scope
}

// runSurface holds the active tool definitions, schemas, registry, and
// scope for one run iteration. Loop holds the immutable baseline.
type runSurface struct {
	defs    []provider.ToolDefinition
	schemas map[string]*schema.Compiled
	reg     *tools.Registry
	scope   *tools.Scope
}

// initialSurface returns the baseline runSurface from l's immutable fields.
func (l *Loop) initialSurface() runSurface {
	return runSurface{
		defs:    l.defs,
		schemas: l.schemas,
		reg:     l.reg,
		scope:   l.scope,
	}
}

// applySurface creates a new runSurface by applying s over current.
// A nil s returns current unchanged.
func applySurface(current runSurface, s *Surface) (runSurface, error) {
	if s == nil {
		return current, nil
	}
	schemas, err := compileSchemas(s.Advertised)
	if err != nil {
		return current, err
	}
	next := runSurface{
		defs:    s.Advertised,
		schemas: schemas,
		reg:     current.reg,
		scope:   current.scope,
	}
	if s.Registry != nil {
		next.reg = s.Registry
	}
	if s.Scope != nil {
		next.scope = s.Scope
	}
	return next, nil
}

// safeSurface invokes fn, converting a panic into a plain error so a
// hostile host hook fails the run closed instead of crashing the
// process. The panic value is rendered into the message for
// diagnostics; no new exported error type enters the package surface.
func safeSurface(fn func() *Surface) (surface *Surface, err error) {
	defer func() {
		if r := recover(); r != nil {
			surface = nil
			err = fmt.Errorf("agentloop: Surface hook panicked: %v", r)
		}
	}()
	return fn(), nil
}
