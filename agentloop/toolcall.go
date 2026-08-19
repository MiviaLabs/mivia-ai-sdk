package agentloop

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
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
		msg, veto, err := l.runOneToolCall(ctx, call, iteration)
		if err != nil {
			return history, false, err
		}
		if veto {
			return history, true, nil
		}
		history = append(history, msg)
	}
	return history, false, nil
}

// runOneToolCall fires PointPreTool, decodes and runs one tool call,
// fires PointPostTool, and renders the result into a RoleTool
// message. A tool-run error, including a DecodeArguments failure,
// goes through l.onToolError: ErrorPolicyReport renders the error
// text as the tool result; ErrorPolicyFail returns the error, wrapped
// with call.ID and iteration, as Run's own hard failure.
func (l *Loop) runOneToolCall(ctx context.Context, call provider.ToolCall, iteration int) (provider.Message, bool, error) {
	if l.tracer != nil {
		var span *trace.Span
		ctx, span = l.tracer.Start(ctx, "agentloop.tool_call")
		defer span.End()
	}

	if l.hooksReg != nil {
		allowed, err := l.fireHook(ctx, hooks.PointPreTool, call)
		if err != nil {
			return provider.Message{}, false, fmt.Errorf("agentloop: iteration %d: tool call %s: %w", iteration, call.ID, err)
		}
		if !allowed {
			return provider.Message{}, true, nil
		}
	}

	t, out, runErr := l.decodeAndRun(ctx, call)

	if l.hooksReg != nil {
		_, _ = l.fireHook(ctx, hooks.PointPostTool, call)
	}

	if runErr != nil {
		if l.onToolError == ErrorPolicyFail {
			return provider.Message{}, false, fmt.Errorf("agentloop: iteration %d: tool call %s: %w", iteration, call.ID, runErr)
		}
		return provider.Message{Role: provider.RoleTool, ToolCallID: call.ID, Content: runErr.Error()}, false, nil
	}

	content, err := l.render(t, out)
	if err != nil {
		if l.onToolError == ErrorPolicyFail {
			return provider.Message{}, false, fmt.Errorf("agentloop: iteration %d: tool call %s: %w", iteration, call.ID, err)
		}
		return provider.Message{Role: provider.RoleTool, ToolCallID: call.ID, Content: err.Error()}, false, nil
	}
	return provider.Message{Role: provider.RoleTool, ToolCallID: call.ID, Content: content}, false, nil
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

// decodeAndRun resolves call.Name, checks l.scope, decodes
// call.Arguments through the resolved tool's DecodeArguments, and
// calls RunScoped. It returns an error wrapping tools.ErrUnknownName
// for an unresolved name and tools.ErrScopeDenied for a name l.scope
// excludes, both before ever calling DecodeArguments or RunScoped: a
// scope-denied tool's decoder must never see model-supplied bytes.
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
	in, err := st.DecodeArguments(call.Arguments)
	if err != nil {
		return t, tools.Out{}, fmt.Errorf("agentloop: tool call %s: decode arguments: %w", call.ID, err)
	}
	out, err := l.reg.RunScoped(ctx, call.Name, in, l.scope)
	return t, out, err
}
