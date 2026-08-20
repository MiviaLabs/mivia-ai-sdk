package runconfig

import (
	"context"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// stepTool adapts one resolved tool to its step ID, so the built ack
// chain runs it by that ID. It never declares tools.ProfiledTool,
// tools.ResultBudgetTool, tools.PrivilegedTool, or tools.SchemaTool
// itself; newStepTool composes one of the wrapper variants below so
// the returned tools.Tool implements exactly the optional interfaces
// inner does.
type stepTool struct {
	step  string
	inner tools.Tool
}

// Name returns the bound step ID.
func (s *stepTool) Name() string { return s.step }

// Run delegates to the resolved tool.
func (s *stepTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return s.inner.Run(ctx, in)
}

// stepProfiledCap adds ExecutionProfile, forwarded from inner through
// tools.ExecutionProfileOf.
type stepProfiledCap struct{ inner tools.Tool }

// ExecutionProfile forwards inner's ExecutionProfile through
// tools.ExecutionProfileOf.
func (c stepProfiledCap) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfileOf(c.inner)
}

// stepBudgetCap adds MaxResultBytes, forwarded from inner through
// tools.ResultBudgetOf.
type stepBudgetCap struct{ inner tools.Tool }

// MaxResultBytes forwards inner's MaxResultBytes through
// tools.ResultBudgetOf.
func (c stepBudgetCap) MaxResultBytes() int {
	n, _ := tools.ResultBudgetOf(c.inner)
	return n
}

// stepPrivilegedCap adds Privileged, forwarded from inner through
// tools.IsPrivileged.
type stepPrivilegedCap struct{ inner tools.Tool }

// Privileged forwards inner's Privileged through tools.IsPrivileged.
func (c stepPrivilegedCap) Privileged() bool {
	return tools.IsPrivileged(c.inner)
}

// stepSchemaCap adds ParameterSchema and DecodeArguments, forwarded
// from inner through tools.SchemaOf and inner's own DecodeArguments.
// A wrapper that stripped this capability would silently make inner
// undecodable to agentrun's chain, since chain checks the wrapper,
// not inner, for tools.SchemaTool.
type stepSchemaCap struct{ inner tools.Tool }

// ParameterSchema forwards inner's schema through tools.SchemaOf.
func (c stepSchemaCap) ParameterSchema() []byte {
	schema, _ := tools.SchemaOf(c.inner)
	return schema
}

// DecodeArguments forwards raw to inner's own DecodeArguments. c.inner
// is known to implement tools.SchemaTool whenever stepSchemaCap is
// composed onto a wrapper variant, so the type assertion always
// succeeds there.
func (c stepSchemaCap) DecodeArguments(raw []byte) (tools.InOut, error) {
	return c.inner.(tools.SchemaTool).DecodeArguments(raw)
}

// The sixteen stepTool variants below embed *stepTool plus exactly
// the capability structs matching the optional interfaces inner
// implements, so a type assertion against tools.ProfiledTool,
// tools.ResultBudgetTool, tools.PrivilegedTool, or tools.SchemaTool on
// the returned tools.Tool succeeds only when inner itself satisfies
// it.

type stepToolPlain struct{ *stepTool }

type stepToolProfiled struct {
	*stepTool
	stepProfiledCap
}

type stepToolBudget struct {
	*stepTool
	stepBudgetCap
}

type stepToolPrivileged struct {
	*stepTool
	stepPrivilegedCap
}

type stepToolSchema struct {
	*stepTool
	stepSchemaCap
}

type stepToolProfiledBudget struct {
	*stepTool
	stepProfiledCap
	stepBudgetCap
}

type stepToolProfiledPrivileged struct {
	*stepTool
	stepProfiledCap
	stepPrivilegedCap
}

type stepToolProfiledSchema struct {
	*stepTool
	stepProfiledCap
	stepSchemaCap
}

type stepToolBudgetPrivileged struct {
	*stepTool
	stepBudgetCap
	stepPrivilegedCap
}

type stepToolBudgetSchema struct {
	*stepTool
	stepBudgetCap
	stepSchemaCap
}

type stepToolPrivilegedSchema struct {
	*stepTool
	stepPrivilegedCap
	stepSchemaCap
}

type stepToolProfiledBudgetPrivileged struct {
	*stepTool
	stepProfiledCap
	stepBudgetCap
	stepPrivilegedCap
}

type stepToolProfiledBudgetSchema struct {
	*stepTool
	stepProfiledCap
	stepBudgetCap
	stepSchemaCap
}

type stepToolProfiledPrivilegedSchema struct {
	*stepTool
	stepProfiledCap
	stepPrivilegedCap
	stepSchemaCap
}

type stepToolBudgetPrivilegedSchema struct {
	*stepTool
	stepBudgetCap
	stepPrivilegedCap
	stepSchemaCap
}

type stepToolAll struct {
	*stepTool
	stepProfiledCap
	stepBudgetCap
	stepPrivilegedCap
	stepSchemaCap
}

// newStepTool adapts inner to its step name and forwards exactly the
// optional tools.Tool interfaces inner implements: tools.SchemaTool,
// tools.ProfiledTool, tools.ResultBudgetTool, and tools.PrivilegedTool.
// A caller-set tools.Scope approval threshold, or a privileged inner
// tool, reads inner's own published capability, not a stripped
// default, once the wrapped tool sits in the registry chain drives.
func newStepTool(step string, inner tools.Tool) tools.Tool {
	base := &stepTool{step: step, inner: inner}

	_, profiled := inner.(tools.ProfiledTool)
	_, budgeted := inner.(tools.ResultBudgetTool)
	_, privileged := inner.(tools.PrivilegedTool)
	_, schemaed := inner.(tools.SchemaTool)

	p := stepProfiledCap{inner}
	b := stepBudgetCap{inner}
	r := stepPrivilegedCap{inner}
	s := stepSchemaCap{inner}

	switch {
	case profiled && budgeted && privileged && schemaed:
		return &stepToolAll{stepTool: base, stepProfiledCap: p, stepBudgetCap: b, stepPrivilegedCap: r, stepSchemaCap: s}
	case profiled && budgeted && privileged:
		return &stepToolProfiledBudgetPrivileged{stepTool: base, stepProfiledCap: p, stepBudgetCap: b, stepPrivilegedCap: r}
	case profiled && budgeted && schemaed:
		return &stepToolProfiledBudgetSchema{stepTool: base, stepProfiledCap: p, stepBudgetCap: b, stepSchemaCap: s}
	case profiled && privileged && schemaed:
		return &stepToolProfiledPrivilegedSchema{stepTool: base, stepProfiledCap: p, stepPrivilegedCap: r, stepSchemaCap: s}
	case budgeted && privileged && schemaed:
		return &stepToolBudgetPrivilegedSchema{stepTool: base, stepBudgetCap: b, stepPrivilegedCap: r, stepSchemaCap: s}
	case profiled && budgeted:
		return &stepToolProfiledBudget{stepTool: base, stepProfiledCap: p, stepBudgetCap: b}
	case profiled && privileged:
		return &stepToolProfiledPrivileged{stepTool: base, stepProfiledCap: p, stepPrivilegedCap: r}
	case profiled && schemaed:
		return &stepToolProfiledSchema{stepTool: base, stepProfiledCap: p, stepSchemaCap: s}
	case budgeted && privileged:
		return &stepToolBudgetPrivileged{stepTool: base, stepBudgetCap: b, stepPrivilegedCap: r}
	case budgeted && schemaed:
		return &stepToolBudgetSchema{stepTool: base, stepBudgetCap: b, stepSchemaCap: s}
	case privileged && schemaed:
		return &stepToolPrivilegedSchema{stepTool: base, stepPrivilegedCap: r, stepSchemaCap: s}
	case profiled:
		return &stepToolProfiled{stepTool: base, stepProfiledCap: p}
	case budgeted:
		return &stepToolBudget{stepTool: base, stepBudgetCap: b}
	case privileged:
		return &stepToolPrivileged{stepTool: base, stepPrivilegedCap: r}
	case schemaed:
		return &stepToolSchema{stepTool: base, stepSchemaCap: s}
	default:
		return &stepToolPlain{stepTool: base}
	}
}
