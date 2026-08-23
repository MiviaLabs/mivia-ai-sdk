package agentloop

import (
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
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
	// Scope narrows Registry lookups this iteration. Nil means
	// unscoped.
	Scope *tools.Scope
}

// apply installs s onto the loop: swaps defs/schemas/scope for the
// values s carries. A nil s is a caller-signaled keep of the prior
// surface. Schema compilation errors surface as Run failures.
func (l *Loop) apply(s *Surface) error {
	if s == nil {
		return nil
	}
	schemas, err := compileSchemas(s.Advertised)
	if err != nil {
		return err
	}
	l.defs = s.Advertised
	l.schemas = schemas
	if s.Registry != nil {
		l.reg = s.Registry
	}
	if s.Scope != nil {
		l.scope = s.Scope
	}
	return nil
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
