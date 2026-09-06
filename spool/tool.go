package spool

import (
	"context"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// spoolTool wraps inner, spooling an oversized string result to store
// under the ctx principal. It never declares tools.ProfiledTool,
// tools.ResultBudgetTool, tools.PrivilegedTool, or tools.SchemaTool
// itself. SpoolTool embeds it in one of the two variants below, which
// add those interfaces.
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
// composed onto the schema struct, so the type assertion always
// succeeds there.
func (c schemaCap) DecodeArguments(raw []byte) (tools.InOut, error) {
	return c.inner.(tools.SchemaTool).DecodeArguments(raw)
}

// The two variants below embed *spoolTool. spoolToolCaps declares
// tools.ProfiledTool, tools.ResultBudgetTool, and
// tools.PrivilegedTool for every inner. Each helper's
// not-implemented default equals the value the wrapper forwards, so
// no caller in this tree observes a difference. spoolToolSchema adds
// tools.SchemaTool, and SpoolTool returns it only when inner
// implements tools.SchemaTool. That one bit stays conditional
// because agentloop.Definitions skips a tool whose tools.SchemaOf
// reports false. An unconditional declaration would report a nil
// schema as published instead of failing closed.

// Same shape as runconfig/steptool.go, minus the schema bit.
type spoolToolCaps struct{ *spoolTool }

// ExecutionProfile forwards inner's ExecutionProfile through tools.ExecutionProfileOf.
func (t spoolToolCaps) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfileOf(t.inner)
}

// MaxResultBytes forwards inner's MaxResultBytes through tools.ResultBudgetOf.
func (t spoolToolCaps) MaxResultBytes() int {
	n, _ := tools.ResultBudgetOf(t.inner)
	return n
}

// Privileged forwards inner's Privileged through tools.IsPrivileged.
func (t spoolToolCaps) Privileged() bool {
	return tools.IsPrivileged(t.inner)
}

type spoolToolSchema struct {
	spoolToolCaps
	schemaCap
}

// SpoolTool wraps inner so any string result longer than maxBytes
// spools to store under the ctx principal (see WithPrincipal) instead
// of returning in full. The wrapped tool's Out.Value becomes the
// truncated view string; the reference is appended to the view text.
// A result that is not a string, or one at or under maxBytes, passes
// through unchanged. A call with no principal in ctx returns
// ErrNoPrincipal.
// The returned tools.Tool always implements tools.ProfiledTool,
// tools.ResultBudgetTool, and tools.PrivilegedTool. Each forwards
// through tools.ExecutionProfileOf, tools.ResultBudgetOf, and
// tools.IsPrivileged, so a caller reading inner's published values
// through those helpers sees no difference. It implements
// tools.SchemaTool only when inner does. That one bit stays
// conditional because agentloop.Definitions skips a tool whose
// tools.SchemaOf reports false; an unconditional declaration would
// offer the model a nil schema instead of failing closed.
// SpoolTool changes only Run's result handling, not inner's declared
// execution class, result budget, privilege, or schema.
// A nil sp wraps ErrNilSpool. A negative maxBytes clamps to zero.
// Two or more SpoolTool calls sharing one sp share its grant budget
// and its Load-time principal checks. A caller pairs a SpoolTool call
// with a ReadOutputTool call by passing the same sp to both.
func SpoolTool(name string, maxBytes int, sp *Spool, inner tools.Tool) (tools.Tool, error) {
	if sp == nil {
		return nil, fmt.Errorf("%w", ErrNilSpool)
	}
	if maxBytes < 0 {
		maxBytes = 0
	}
	caps := spoolToolCaps{&spoolTool{name: name, maxBytes: maxBytes, sp: sp, inner: inner}}

	if _, ok := inner.(tools.SchemaTool); ok {
		return &spoolToolSchema{spoolToolCaps: caps, schemaCap: schemaCap{inner}}, nil
	}
	return &caps, nil
}
