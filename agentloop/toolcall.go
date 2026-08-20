package agentloop

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/schema"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/trace"
)

// runToolCalls runs calls in ToolCall.Index order, sequentially,
// appending one RoleTool message per call onto history. It stops
// early and returns veto == true, with no error, on a PointPreTool
// veto: the tool that call names does not run, and no later call in
// this turn runs either. It also stops early, with ctx.Err() as the
// returned error, when ctx is canceled ahead of a call: a canceled
// run must not keep executing tool calls it can still skip.
func (l *Loop) runToolCalls(ctx context.Context, history []provider.Message, calls []provider.ToolCall, iteration int) ([]provider.Message, bool, error) {
	ordered := append([]provider.ToolCall(nil), calls...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Index < ordered[j].Index })

	for _, call := range ordered {
		if err := ctx.Err(); err != nil {
			return history, false, err
		}
		msg, veto, reported, err := l.runOneToolCall(ctx, call, iteration)
		if err != nil {
			return history, false, err
		}
		if veto {
			return history, true, nil
		}
		history = append(history, msg)
		if l.audit != nil {
			if err := l.audit(ctx, AuditRecord{
				Iteration:  iteration,
				Kind:       AuditKindToolCall,
				ToolCall:   call,
				ToolResult: msg,
				Err:        reported,
			}); err != nil {
				return history, false, fmt.Errorf("agentloop: iteration %d: audit: %w", iteration, err)
			}
		}
	}
	return history, false, nil
}

// runOneToolCall fires PointPreTool, decodes and runs one tool call,
// fires PointPostTool, and renders the result into a RoleTool
// message. A tool-run error, including a DecodeArguments or
// schema-validation failure, goes through l.onToolError:
// ErrorPolicyReport renders the error text, ToolErrorPrefix-marked,
// as the tool result and returns that error as reported;
// ErrorPolicyFail returns the error, wrapped with call.ID and
// iteration, as Run's own hard failure, with reported nil. reported
// is also nil on a successful call. EventToolCallStart fires before
// the PointPreTool fire; EventToolCallEnd fires from a deferred
// closure covering every return path, including a veto and a
// PointPreTool hook error, so both bracket the whole call, not only
// its blocking segment. A heartbeat ticker for EventToolCallHeartbeat
// starts only after the veto check passes.
func (l *Loop) runOneToolCall(ctx context.Context, call provider.ToolCall, iteration int) (msg provider.Message, veto bool, reported error, err error) {
	label := toolCallLabel(iteration, call)
	l.emitEvent(ctx, EventToolCallStart, label)
	defer func() { l.emitEvent(ctx, EventToolCallEnd, label) }()

	if l.tracer != nil {
		var span *trace.Span
		ctx, span = l.tracer.Start(ctx, "agentloop.tool_call")
		defer span.End()
	}

	if l.hooksReg != nil {
		allowed, hookErr := l.fireHook(ctx, hooks.PointPreTool, call)
		if hookErr != nil {
			return provider.Message{}, false, nil, fmt.Errorf("agentloop: iteration %d: tool call %s: %w", iteration, call.ID, hookErr)
		}
		if !allowed {
			return provider.Message{}, true, nil, nil
		}
	}

	stopHeartbeat := l.startHeartbeat(ctx, EventToolCallHeartbeat, label)
	defer stopHeartbeat()
	t, out, runErr := l.decodeAndRun(ctx, call)

	if l.hooksReg != nil {
		_, _ = l.fireHook(ctx, hooks.PointPostTool, call)
	}

	if runErr != nil {
		if l.onToolError == ErrorPolicyFail {
			return provider.Message{}, false, nil, fmt.Errorf("agentloop: iteration %d: tool call %s: %w", iteration, call.ID, runErr)
		}
		content := errorReportContent(runErr)
		return provider.Message{Role: provider.RoleTool, ToolCallID: call.ID, Content: content}, false, runErr, nil
	}

	content, renderErr := l.render(t, out)
	if renderErr != nil {
		if l.onToolError == ErrorPolicyFail {
			return provider.Message{}, false, nil, fmt.Errorf("agentloop: iteration %d: tool call %s: %w", iteration, call.ID, renderErr)
		}
		return provider.Message{Role: provider.RoleTool, ToolCallID: call.ID, Content: ToolErrorPrefix + renderErr.Error()}, false, renderErr, nil
	}
	return provider.Message{Role: provider.RoleTool, ToolCallID: call.ID, Content: content}, false, nil, nil
}

// errorReportContent renders a decodeAndRun error's ErrorPolicyReport
// content: an ErrArgumentValidation failure renders through
// schema.Corrective(err), the bounded, schema-derived corrective
// message; every other error renders its own Error() text. Both cases
// carry the ToolErrorPrefix marker.
func errorReportContent(err error) string {
	if errors.Is(err, ErrArgumentValidation) {
		return ToolErrorPrefix + schema.Corrective(err)
	}
	return ToolErrorPrefix + err.Error()
}

// fireHook fires point through l.hooksReg and turns a veto,
// distinguished by errors.Is against hooks.ErrVetoed, into (false,
// nil). Any other handler error passes through unchanged.
func (l *Loop) fireHook(ctx context.Context, point hooks.Point, call provider.ToolCall) (bool, error) {
	err := l.hooksReg.Fire(ctx, point, call)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, hooks.ErrVetoed) {
		return false, nil
	}
	return false, err
}

// decodeAndRun resolves call.Name, checks l.scope, validates
// call.Arguments against l.schemas[call.Name], decodes them through
// the resolved tool's DecodeArguments, and calls RunScoped. It
// returns an error wrapping tools.ErrUnknownName for an unresolved
// name and tools.ErrScopeDenied for a name l.scope excludes, both
// before ever calling schema.Compiled.Validate, DecodeArguments, or
// RunScoped: a scope-denied tool's decoder must never see
// model-supplied bytes. l.schemas[call.Name] hits whenever call.Name
// was in the Scope-offered set New compiled from. A miss means a
// tool the caller registered on the shared *tools.Registry after New
// ran: reg.Get and l.scope.Allowed both read the live registry and
// the live scope, so the call still reaches this point, but l.schemas,
// frozen at New, carries no entry for it. decodeAndRun returns
// ErrToolNotOffered in that case, instead of indexing a nil
// *schema.Compiled.
func (l *Loop) decodeAndRun(ctx context.Context, call provider.ToolCall) (tools.Tool, tools.Out, error) {
	t, ok := l.reg.Get(call.Name)
	if !ok {
		return nil, tools.Out{}, fmt.Errorf("agentloop: tool call %s: %w", call.ID, tools.ErrUnknownName)
	}
	if l.scope != nil && !l.scope.Allowed(call.Name, t) {
		return t, tools.Out{}, fmt.Errorf("agentloop: tool call %s: %w", call.ID, tools.ErrScopeDenied)
	}
	st, ok := t.(tools.SchemaTool)
	if !ok {
		return t, tools.Out{}, fmt.Errorf("agentloop: tool %q publishes no schema", call.Name)
	}
	compiled, ok := l.schemas[call.Name]
	if !ok {
		return t, tools.Out{}, fmt.Errorf("agentloop: tool call %s: %w", call.ID, ErrToolNotOffered)
	}
	if err := compiled.Validate(call.Arguments); err != nil {
		return t, tools.Out{}, fmt.Errorf("agentloop: tool call %s: %w: %w", call.ID, ErrArgumentValidation, err)
	}
	in, err := st.DecodeArguments(call.Arguments)
	if err != nil {
		return t, tools.Out{}, fmt.Errorf("agentloop: tool call %s: decode arguments: %w", call.ID, err)
	}
	out, err := l.reg.RunScoped(ctx, call.Name, in, l.scope)
	return t, out, err
}
