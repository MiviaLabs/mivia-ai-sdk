package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/schema"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/trace"
)

// dedupKey identifies one (tool name, canonical arguments) pair within
// one turn's dedup set. See canonicalizeArgs for the canonical form.
type dedupKey struct {
	name string
	args string
}

// runToolCalls runs calls in ToolCall.Index order, sequentially,
// appending one RoleTool message per call onto history. It stops
// early and returns veto == true, with no error, on a PointPreTool
// veto: the tool that call names does not run, and no later call in
// this turn runs either. It also stops early, with ctx.Err() as the
// returned error, when ctx is canceled ahead of a call: a canceled
// run must not keep executing tool calls it can still skip.
//
// When l.dedupWithinTurn is true, a per-call dedup set, keyed by
// dedupKey, starts empty on every call to runToolCalls (one call per
// turn), so no dedup carries across iterations. A call whose
// (name, canonical-argument) pair is already in the set never reaches
// runOneToolCall: it short-circuits before PointPreTool, serving
// DuplicateCallNotice with the duplicate call's own ToolCallID
// instead. A call excluded from the set by a canonicalizeArgs error
// always runs and is never recorded. Any call that does run, success
// or ErrorPolicyReport-driven error, records its pair in the set once
// its RoleTool message reaches history, so a later identical retry in
// the same turn is deduped either way. A deduped call's
// DuplicateCallNotice content never counts toward the turn's byte
// total below, since it never reaches runOneToolCall or the shaping
// step.
//
// When l.turnResultBudget is positive, runToolCalls shapes each call's
// already-rendered msg.Content against a running byte total for this
// turn, after runOneToolCall's own per-call tools.ResultBudgetOf bound
// already applied. A call's content stays whole only when the running
// total plus its byte length does not exceed l.turnResultBudget;
// otherwise the content is replaced with BatchTruncationNotice and the
// running total does not grow for it. AuditRecord.Err always reports
// the true per-call outcome, independent of this shaping. The running
// total resets to zero once per runToolCalls call, at the start of
// this turn's batch. l.turnResultBudget zero skips the check entirely.
func (l *Loop) runToolCalls(ctx context.Context, history []provider.Message, calls []provider.ToolCall, iteration int) ([]provider.Message, bool, error) {
	ordered := append([]provider.ToolCall(nil), calls...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Index < ordered[j].Index })
	if len(ordered) > 1 {
		l.emitEvent(ctx, EventToolParallel, parallelLabel(len(ordered)))
	}

	seen := make(map[dedupKey]struct{})
	runningTotal := 0
	for _, call := range ordered {
		if err := ctx.Err(); err != nil {
			return history, false, err
		}

		key, eligible := l.dedupKeyFor(call)
		if eligible {
			if _, dup := seen[key]; dup {
				msg := provider.Message{Role: provider.RoleTool, ToolCallID: call.ID, Name: call.Name, Content: DuplicateCallNotice}
				history = append(history, msg)
				if err := l.auditToolCall(ctx, iteration, call, msg, nil); err != nil {
					return history, false, err
				}
				continue
			}
		}

		msg, veto, reported, err := l.runOneToolCall(ctx, call, iteration)
		if err != nil {
			return history, false, err
		}
		if veto {
			return history, true, nil
		}
		if l.turnResultBudget > 0 {
			if runningTotal+len(msg.Content) <= l.turnResultBudget {
				runningTotal += len(msg.Content)
			} else {
				msg.Content = BatchTruncationNotice
			}
		}
		history = append(history, msg)
		if eligible {
			seen[key] = struct{}{}
		}
		if err := l.auditToolCall(ctx, iteration, call, msg, reported); err != nil {
			return history, false, err
		}
	}
	return history, false, nil
}

// dedupKeyFor builds call's dedup key when l.dedupWithinTurn is true
// and call.Arguments canonicalizes without error. eligible is false
// when dedup is off or canonicalization fails; a canonicalization
// failure is fail-open, per canonicalizeArgs's contract: the call
// still runs, unconditionally, and is never recorded in or matched
// against the dedup set.
func (l *Loop) dedupKeyFor(call provider.ToolCall) (dedupKey, bool) {
	if !l.dedupWithinTurn {
		return dedupKey{}, false
	}
	canon, err := canonicalizeArgs(json.RawMessage(call.Arguments))
	if err != nil {
		return dedupKey{}, false
	}
	return dedupKey{name: call.Name, args: canon}, true
}

// auditToolCall reports one AuditKindToolCall record through l.audit,
// when set, wrapping a non-nil handler error with iteration.
func (l *Loop) auditToolCall(ctx context.Context, iteration int, call provider.ToolCall, msg provider.Message, reported error) error {
	if l.audit == nil {
		return nil
	}
	if err := l.audit(ctx, AuditRecord{
		Iteration:  iteration,
		Kind:       AuditKindToolCall,
		ToolCall:   call,
		ToolResult: msg,
		Err:        reported,
	}); err != nil {
		return fmt.Errorf("agentloop: iteration %d: audit: %w", iteration, err)
	}
	return nil
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
	t, out, runErr := func() (tools.Tool, tools.Out, error) {
		defer stopHeartbeat()
		return l.decodeAndRun(ctx, call)
	}()

	if l.hooksReg != nil {
		_, _ = l.fireHook(ctx, hooks.PointPostTool, call)
	}

	if runErr != nil {
		if l.onToolError == ErrorPolicyFail {
			return provider.Message{}, false, nil, fmt.Errorf("agentloop: iteration %d: tool call %s: %w", iteration, call.ID, runErr)
		}
		content := errorReportContent(runErr)
		return provider.Message{Role: provider.RoleTool, ToolCallID: call.ID, Name: call.Name, Content: content}, false, runErr, nil
	}

	content, renderErr := l.render(t, out)
	if renderErr != nil {
		if l.onToolError == ErrorPolicyFail {
			return provider.Message{}, false, nil, fmt.Errorf("agentloop: iteration %d: tool call %s: %w", iteration, call.ID, renderErr)
		}
		return provider.Message{Role: provider.RoleTool, ToolCallID: call.ID, Name: call.Name, Content: ToolErrorPrefix + renderErr.Error()}, false, renderErr, nil
	}
	return provider.Message{Role: provider.RoleTool, ToolCallID: call.ID, Name: call.Name, Content: content}, false, nil, nil
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
