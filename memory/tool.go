package memory

import (
	"context"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// spoolTool wraps inner, spooling an oversized string result to sp
// under the ctx principal. It declares all four optional tools.Tool
// capability interfaces for every inner; each capability forwards
// through the matching tools helper and degrades to inner's published
// default when inner lacks the interface. Same shape as
// runconfig/steptool.go.
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

// ExecutionProfile forwards inner's ExecutionProfile through
// tools.ExecutionProfileOf.
func (t *spoolTool) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfileOf(t.inner)
}

// MaxResultBytes forwards inner's MaxResultBytes through
// tools.ResultBudgetOf.
func (t *spoolTool) MaxResultBytes() int {
	n, _ := tools.ResultBudgetOf(t.inner)
	return n
}

// Privileged forwards inner's Privileged through tools.IsPrivileged.
func (t *spoolTool) Privileged() bool {
	return tools.IsPrivileged(t.inner)
}

// ParameterSchema forwards inner's schema through tools.SchemaOf. A
// schema-less inner yields nil: tools.SchemaOf fails closed on nil
// schema bytes, whether or not inner implements tools.SchemaTool.
func (t *spoolTool) ParameterSchema() []byte {
	schema, _ := tools.SchemaOf(t.inner)
	return schema
}

// DecodeArguments forwards raw to inner when inner implements
// tools.SchemaTool. Otherwise it identity-decodes raw as a string
// value, matching the plain-payload pass-through byte for byte.
func (t *spoolTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	if st, ok := t.inner.(tools.SchemaTool); ok {
		return st.DecodeArguments(raw)
	}
	return tools.InOut{Value: string(raw)}, nil
}

// SpoolTool wraps inner so any string result longer than maxBytes
// spools to store under the ctx principal (see WithPrincipal) instead
// of returning in full. The wrapped tool's Out.Value becomes the
// truncated view string; the reference is appended to the view text.
// A result that is not a string, or one at or under maxBytes, passes
// through unchanged. A call with no principal in ctx returns
// ErrNoPrincipal.
// The returned tools.Tool always implements tools.ProfiledTool,
// tools.ResultBudgetTool, tools.PrivilegedTool, and tools.SchemaTool.
// Each forwards through tools.ExecutionProfileOf, tools.ResultBudgetOf,
// tools.IsPrivileged, and tools.SchemaOf, so a caller reading inner's
// published values through those helpers sees no difference.
// tools.SchemaOf fails closed: a schema-less inner reports nil, false,
// so agentloop.Definitions skips the wrapper instead of offering a nil
// schema. SpoolTool changes only Run's result handling, not inner's
// declared execution class, result budget, privilege, or schema.
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
	return &spoolTool{name: name, maxBytes: maxBytes, sp: sp, inner: inner}, nil
}
