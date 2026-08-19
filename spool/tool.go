package spool

import (
	"context"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// toolGrantBudget bounds the internal Spool a SpoolTool wrapper
// builds to hold its own oversized results. It is a fixed, generous
// budget; a caller who needs a tighter or shared budget spools
// through a Spool value of its own instead of SpoolTool.
const toolGrantBudget = 64 * 1024 * 1024

// spoolTool wraps inner, spooling an oversized string result to store
// under the ctx principal. It never declares tools.ProfiledTool,
// tools.ResultBudgetTool, tools.PrivilegedTool, or tools.SchemaTool
// itself; SpoolTool composes one of the wrapper variants below so the
// returned tools.Tool implements exactly the optional interfaces
// inner does.
type spoolTool struct {
	name     string
	maxBytes int
	sp       *Spool
	inner    tools.Tool
}

// Name returns t's registry name.
func (t *spoolTool) Name() string { return t.name }

// Run calls t.inner.Run and spools an oversized string result.
func (t *spoolTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	out, err := t.inner.Run(ctx, in)
	if err != nil {
		return out, err
	}

	s, ok := out.Value.(string)
	if !ok || len(s) <= t.maxBytes {
		return out, nil
	}

	principal, ok := PrincipalFrom(ctx)
	if !ok {
		return tools.Out{}, ErrNoPrincipal
	}

	_, ref, err := t.sp.Spool(ctx, principal, []byte(s))
	if err != nil {
		return tools.Out{}, err
	}
	return tools.Out{Value: buildView([]byte(s), t.maxBytes, ref)}, nil
}

// profiledCap adds ExecutionProfile, forwarded from inner through
// tools.ExecutionProfileOf.
type profiledCap struct{ inner tools.Tool }

// ExecutionProfile forwards inner's ExecutionProfile through
// tools.ExecutionProfileOf.
func (c profiledCap) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfileOf(c.inner)
}

// budgetCap adds MaxResultBytes, forwarded from inner through
// tools.ResultBudgetOf.
type budgetCap struct{ inner tools.Tool }

// MaxResultBytes forwards inner's MaxResultBytes through
// tools.ResultBudgetOf.
func (c budgetCap) MaxResultBytes() int {
	n, _ := tools.ResultBudgetOf(c.inner)
	return n
}

// privilegedCap adds Privileged, forwarded from inner through
// tools.IsPrivileged.
type privilegedCap struct{ inner tools.Tool }

// Privileged forwards inner's Privileged through tools.IsPrivileged.
func (c privilegedCap) Privileged() bool {
	return tools.IsPrivileged(c.inner)
}

// schemaCap adds ParameterSchema and DecodeArguments, forwarded from
// inner through tools.SchemaOf and inner's own DecodeArguments. A
// wrapper that stripped this capability would silently make inner
// unreachable to an agentloop.Loop's model, since
// agentloop.Definitions skips a tool with no published schema.
type schemaCap struct{ inner tools.Tool }

// ParameterSchema forwards inner's schema through tools.SchemaOf.
func (c schemaCap) ParameterSchema() []byte {
	schema, _ := tools.SchemaOf(c.inner)
	return schema
}

// DecodeArguments forwards raw to inner's own DecodeArguments. c.inner
// is known to implement tools.SchemaTool whenever schemaCap is
// composed onto a wrapper variant, so the type assertion always
// succeeds there.
func (c schemaCap) DecodeArguments(raw []byte) (tools.InOut, error) {
	return c.inner.(tools.SchemaTool).DecodeArguments(raw)
}

// The sixteen spoolTool variants below embed *spoolTool plus exactly
// the capability structs matching the optional interfaces inner
// implements, so a type assertion against tools.ProfiledTool,
// tools.ResultBudgetTool, tools.PrivilegedTool, or tools.SchemaTool on
// the returned tools.Tool succeeds only when inner itself satisfies
// it.

type spoolToolPlain struct{ *spoolTool }

type spoolToolProfiled struct {
	*spoolTool
	profiledCap
}

type spoolToolBudget struct {
	*spoolTool
	budgetCap
}

type spoolToolPrivileged struct {
	*spoolTool
	privilegedCap
}

type spoolToolSchema struct {
	*spoolTool
	schemaCap
}

type spoolToolProfiledBudget struct {
	*spoolTool
	profiledCap
	budgetCap
}

type spoolToolProfiledPrivileged struct {
	*spoolTool
	profiledCap
	privilegedCap
}

type spoolToolProfiledSchema struct {
	*spoolTool
	profiledCap
	schemaCap
}

type spoolToolBudgetPrivileged struct {
	*spoolTool
	budgetCap
	privilegedCap
}

type spoolToolBudgetSchema struct {
	*spoolTool
	budgetCap
	schemaCap
}

type spoolToolPrivilegedSchema struct {
	*spoolTool
	privilegedCap
	schemaCap
}

type spoolToolProfiledBudgetPrivileged struct {
	*spoolTool
	profiledCap
	budgetCap
	privilegedCap
}

type spoolToolProfiledBudgetSchema struct {
	*spoolTool
	profiledCap
	budgetCap
	schemaCap
}

type spoolToolProfiledPrivilegedSchema struct {
	*spoolTool
	profiledCap
	privilegedCap
	schemaCap
}

type spoolToolBudgetPrivilegedSchema struct {
	*spoolTool
	budgetCap
	privilegedCap
	schemaCap
}

type spoolToolAll struct {
	*spoolTool
	profiledCap
	budgetCap
	privilegedCap
	schemaCap
}

// SpoolTool wraps inner so any string result longer than maxBytes
// spools to store under the ctx principal (see WithPrincipal) instead
// of returning in full. The wrapped tool's Out.Value becomes the
// truncated view string; the reference is appended to the view text.
// A result that is not a string, or one at or under maxBytes, passes
// through unchanged. A call with no principal in ctx returns
// ErrNoPrincipal.
// The returned tools.Tool implements tools.ProfiledTool,
// tools.ResultBudgetTool, tools.PrivilegedTool, and tools.SchemaTool
// only when inner itself does, forwarding each call straight to
// inner: SpoolTool changes only Run's result handling, not inner's
// declared execution class, result budget, privilege, or schema, and
// it never fakes a capability inner does not publish.
func SpoolTool(name string, maxBytes int, store ContentStore, inner tools.Tool) tools.Tool {
	sp := &Spool{
		store:         store,
		maxGrantBytes: toolGrantBudget,
		grants:        make(map[string]grant),
	}
	base := &spoolTool{name: name, maxBytes: maxBytes, sp: sp, inner: inner}

	_, profiled := inner.(tools.ProfiledTool)
	_, budgeted := inner.(tools.ResultBudgetTool)
	_, privileged := inner.(tools.PrivilegedTool)
	_, schemaed := inner.(tools.SchemaTool)

	return buildSpoolTool(base, inner, profiled, budgeted, privileged, schemaed)
}

// buildSpoolTool composes the spoolTool variant matching exactly the
// four booleans, each true when inner implements the matching
// optional interface.
func buildSpoolTool(base *spoolTool, inner tools.Tool, profiled, budgeted, privileged, schemaed bool) tools.Tool {
	p := profiledCap{inner}
	b := budgetCap{inner}
	r := privilegedCap{inner}
	s := schemaCap{inner}

	switch {
	case profiled && budgeted && privileged && schemaed:
		return &spoolToolAll{spoolTool: base, profiledCap: p, budgetCap: b, privilegedCap: r, schemaCap: s}
	case profiled && budgeted && privileged:
		return &spoolToolProfiledBudgetPrivileged{spoolTool: base, profiledCap: p, budgetCap: b, privilegedCap: r}
	case profiled && budgeted && schemaed:
		return &spoolToolProfiledBudgetSchema{spoolTool: base, profiledCap: p, budgetCap: b, schemaCap: s}
	case profiled && privileged && schemaed:
		return &spoolToolProfiledPrivilegedSchema{spoolTool: base, profiledCap: p, privilegedCap: r, schemaCap: s}
	case budgeted && privileged && schemaed:
		return &spoolToolBudgetPrivilegedSchema{spoolTool: base, budgetCap: b, privilegedCap: r, schemaCap: s}
	case profiled && budgeted:
		return &spoolToolProfiledBudget{spoolTool: base, profiledCap: p, budgetCap: b}
	case profiled && privileged:
		return &spoolToolProfiledPrivileged{spoolTool: base, profiledCap: p, privilegedCap: r}
	case profiled && schemaed:
		return &spoolToolProfiledSchema{spoolTool: base, profiledCap: p, schemaCap: s}
	case budgeted && privileged:
		return &spoolToolBudgetPrivileged{spoolTool: base, budgetCap: b, privilegedCap: r}
	case budgeted && schemaed:
		return &spoolToolBudgetSchema{spoolTool: base, budgetCap: b, schemaCap: s}
	case privileged && schemaed:
		return &spoolToolPrivilegedSchema{spoolTool: base, privilegedCap: r, schemaCap: s}
	case profiled:
		return &spoolToolProfiled{spoolTool: base, profiledCap: p}
	case budgeted:
		return &spoolToolBudget{spoolTool: base, budgetCap: b}
	case privileged:
		return &spoolToolPrivileged{spoolTool: base, privilegedCap: r}
	case schemaed:
		return &spoolToolSchema{spoolTool: base, schemaCap: s}
	default:
		return &spoolToolPlain{spoolTool: base}
	}
}
