package runconfig

import (
	"context"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// stepTool adapts one resolved tool to its bound identifier, so the
// built ack chain runs it by that identifier. It declares all four
// optional tools.Tool capability interfaces for every inner tool; each
// capability forwards through the matching tools helper and degrades
// to inner's published default when inner lacks the interface.
type stepTool struct {
	step  string
	inner tools.Tool
}

// Name returns the bound identifier.
func (s *stepTool) Name() string { return s.step }

// Run delegates to the resolved tool.
func (s *stepTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return s.inner.Run(ctx, in)
}

// ExecutionProfile forwards inner's profile through
// tools.ExecutionProfileOf.
func (s *stepTool) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfileOf(s.inner)
}

// MaxResultBytes forwards inner's result budget through
// tools.ResultBudgetOf.
func (s *stepTool) MaxResultBytes() int {
	n, _ := tools.ResultBudgetOf(s.inner)
	return n
}

// Privileged forwards inner's privilege through tools.IsPrivileged.
func (s *stepTool) Privileged() bool {
	return tools.IsPrivileged(s.inner)
}

// ParameterSchema forwards inner's schema through tools.SchemaOf.
func (s *stepTool) ParameterSchema() []byte {
	schema, _ := tools.SchemaOf(s.inner)
	return schema
}

// DecodeArguments forwards raw to inner when inner implements
// tools.SchemaTool. Otherwise it identity-decodes raw as a string
// value, matching the plain-payload pass-through byte for byte.
func (s *stepTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	if st, ok := s.inner.(tools.SchemaTool); ok {
		return st.DecodeArguments(raw)
	}
	return tools.InOut{Value: string(raw)}, nil
}

// newStepTool adapts inner to its bound identifier. The wrapper
// declares tools.SchemaTool, tools.ProfiledTool,
// tools.ResultBudgetTool, and tools.PrivilegedTool for every inner,
// so a caller-set tools.Scope approval threshold, or a privileged
// inner tool, reads inner's own published capability, not a stripped
// default, once the wrapped tool sits in the registry chain drives.
func newStepTool(step string, inner tools.Tool) tools.Tool {
	return &stepTool{step: step, inner: inner}
}
