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
// tools.ResultBudgetTool, or tools.PrivilegedTool itself; SpoolTool
// composes one of the wrapper variants below so the returned
// tools.Tool implements exactly the optional interfaces inner does.
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

// The eight spoolTool variants below embed *spoolTool plus exactly the
// capability structs matching the optional interfaces inner
// implements, so a type assertion against tools.ProfiledTool,
// tools.ResultBudgetTool, or tools.PrivilegedTool on the returned
// tools.Tool succeeds only when inner itself satisfies it.

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

type spoolToolBudgetPrivileged struct {
	*spoolTool
	budgetCap
	privilegedCap
}

type spoolToolAll struct {
	*spoolTool
	profiledCap
	budgetCap
	privilegedCap
}

// SpoolTool wraps inner so any string result longer than maxBytes
// spools to store under the ctx principal (see WithPrincipal) instead
// of returning in full. The wrapped tool's Out.Value becomes the
// truncated view string; the reference is appended to the view text.
// A result that is not a string, or one at or under maxBytes, passes
// through unchanged. A call with no principal in ctx returns
// ErrNoPrincipal.
// The returned tools.Tool implements tools.ProfiledTool,
// tools.ResultBudgetTool, and tools.PrivilegedTool only when inner
// itself does, forwarding each call straight to inner: SpoolTool
// changes only Run's result handling, not inner's declared execution
// class, result budget, or privilege, and it never fakes a budget or
// profile inner does not publish.
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

	switch {
	case profiled && budgeted && privileged:
		return &spoolToolAll{spoolTool: base, profiledCap: profiledCap{inner}, budgetCap: budgetCap{inner}, privilegedCap: privilegedCap{inner}}
	case profiled && budgeted:
		return &spoolToolProfiledBudget{spoolTool: base, profiledCap: profiledCap{inner}, budgetCap: budgetCap{inner}}
	case profiled && privileged:
		return &spoolToolProfiledPrivileged{spoolTool: base, profiledCap: profiledCap{inner}, privilegedCap: privilegedCap{inner}}
	case budgeted && privileged:
		return &spoolToolBudgetPrivileged{spoolTool: base, budgetCap: budgetCap{inner}, privilegedCap: privilegedCap{inner}}
	case profiled:
		return &spoolToolProfiled{spoolTool: base, profiledCap: profiledCap{inner}}
	case budgeted:
		return &spoolToolBudget{spoolTool: base, budgetCap: budgetCap{inner}}
	case privileged:
		return &spoolToolPrivileged{spoolTool: base, privilegedCap: privilegedCap{inner}}
	default:
		return &spoolToolPlain{spoolTool: base}
	}
}
