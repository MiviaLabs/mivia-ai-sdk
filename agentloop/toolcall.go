package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/schema"
	"github.com/MiviaLabs/mivia-ai-sdk/toolcallctx"
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
func (l *Loop) runToolCalls(ctx context.Context, history []provider.Message, calls []provider.ToolCall, iteration int, surface runSurface) ([]provider.Message, bool, bool, error) {
	ordered := append([]provider.ToolCall(nil), calls...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Index < ordered[j].Index })
	if len(ordered) > 1 {
		l.emitEvent(ctx, EventToolParallel, parallelLabel(len(ordered)))
	}

	if err := ctx.Err(); err != nil {
		return history, false, false, err
	}
	plans := l.planCalls(ordered)
	results := l.executeCalls(ctx, plans, iteration, surface)
	return l.collectCalls(ctx, history, plans, results, iteration)
}

// callPlan is one call's pre-dispatch decision: the call itself and,
// when DedupWithinTurn already served an identical (name,
// canonical-argument) pair earlier in this turn, the pre-computed
// DuplicateCallNotice message. duplicate calls never dispatch.
type callPlan struct {
	call      provider.ToolCall
	duplicate bool
	msg       provider.Message
}

// planCalls makes the dedup decision for every call, serially and in
// Index order, before any call dispatches. Under a parallel worker
// pool the reservation must happen ahead of dispatch, otherwise two
// byte-equal calls could both pass the check before either records
// its pair and the tool would run twice.
func (l *Loop) planCalls(ordered []provider.ToolCall) []callPlan {
	plans := make([]callPlan, len(ordered))
	seen := make(map[dedupKey]struct{})
	for i, call := range ordered {
		key, eligible := l.dedupKeyFor(call)
		dup := false
		if eligible {
			_, dup = seen[key]
			seen[key] = struct{}{}
		}
		plans[i] = callPlan{call: call, duplicate: dup,
			msg: provider.Message{Role: provider.RoleTool, ToolCallID: call.ID, Name: call.Name, Content: DuplicateCallNotice}}
	}
	return plans
}

// callOutcome is one dispatched call's runOneToolCall result, stored
// so the collect pass can walk calls in Index order whatever order
// the workers finished in.
type callOutcome struct {
	msg      provider.Message
	veto     bool
	reported error
	err      error
}

// executeCalls dispatches every non-duplicate plan's call, serially
// when l.maxConcurrent < 2 and through a worker pool of size
// l.maxConcurrent otherwise. Dispatch overlap is the only difference
// between the two paths: per-call semantics are runOneToolCall's own.
func (l *Loop) executeCalls(ctx context.Context, plans []callPlan, iteration int, surface runSurface) []callOutcome {
	outcomes := make([]callOutcome, len(plans))
	idx := make([]int, 0, len(plans))
	for i, p := range plans {
		if !p.duplicate {
			idx = append(idx, i)
		}
	}
	var aborted atomic.Bool
	run := func(i int) {
		outcomes[i] = l.oneCallOutcome(ctx, plans[i].call, iteration, surface)
		if outcomes[i].err != nil || outcomes[i].veto {
			aborted.Store(true)
		}
	}
	if l.maxConcurrent < 2 || len(idx) < 2 {
		for _, i := range idx {
			run(i)
			if aborted.Load() {
				break
			}
		}
		return outcomes
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < l.maxConcurrent && w < len(idx); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if aborted.Load() {
					return
				}
				n := int(next.Add(1)) - 1
				if n >= len(idx) {
					return
				}
				run(idx[n])
			}
		}()
	}
	wg.Wait()
	return outcomes
}

// oneCallOutcome runs runOneToolCall for one call and packs the
// result into a callOutcome.
func (l *Loop) oneCallOutcome(ctx context.Context, call provider.ToolCall, iteration int, surface runSurface) callOutcome {
	msg, veto, reported, err := l.runOneToolCall(ctx, call, iteration, surface)
	return callOutcome{msg: msg, veto: veto, reported: reported, err: err}
}

// collectCalls walks the plans in Index order, appending each call's
// message to history: a duplicate plan appends its pre-computed
// DuplicateCallNotice; every other plan appends its dispatched
// outcome, shaped against the turn's running byte budget. History
// order, audit order, and the veto short-circuit therefore match the
// serial path regardless of dispatch overlap.
func (l *Loop) collectCalls(ctx context.Context, history []provider.Message, plans []callPlan, outcomes []callOutcome, iteration int) ([]provider.Message, bool, bool, error) {
	runningTotal := 0
	dispatched := 0
	failed := 0
	for i, p := range plans {
		if p.duplicate {
			history = append(history, p.msg)
			if err := l.auditToolCall(ctx, iteration, p.call, p.msg, nil); err != nil {
				return history, false, false, err
			}
			continue
		}
		dispatched++
		out := outcomes[i]
		if out.err != nil {
			return history, false, false, out.err
		}
		if out.veto {
			return history, true, false, nil
		}
		if errors.Is(out.reported, tools.ErrUnknownName) {
			failed++
		}
		msg := out.msg
		if l.turnResultBudget > 0 {
			if runningTotal+len(msg.Content) <= l.turnResultBudget {
				runningTotal += len(msg.Content)
			} else {
				msg.Content = BatchTruncationNotice
			}
		}
		history = append(history, msg)
		if err := l.auditToolCall(ctx, iteration, p.call, msg, out.reported); err != nil {
			return history, false, false, err
		}
	}
	allFailed := dispatched > 0 && dispatched == failed
	return history, false, allFailed, nil
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
func (l *Loop) runOneToolCall(ctx context.Context, call provider.ToolCall, iteration int, surface runSurface) (msg provider.Message, veto bool, reported error, err error) {
	callCtx := toolcallctx.WithToolCall(ctx, call)
	label := toolCallLabel(iteration, call)
	l.emitEvent(callCtx, EventToolCallStart, label)
	defer func() { l.emitEvent(callCtx, EventToolCallEnd, label) }()

	if l.tracer != nil {
		var span *trace.Span
		callCtx, span = l.tracer.Start(callCtx, "agentloop.tool_call")
		span.SetAttribute("tool.call.id", call.ID)
		defer span.End()
	}

	if l.hooksReg != nil {
		allowed, hookErr := l.fireHook(callCtx, hooks.PointPreTool, call)
		if hookErr != nil {
			return provider.Message{}, false, nil, fmt.Errorf("agentloop: iteration %d: tool call %s: %w", iteration, call.ID, hookErr)
		}
		if !allowed {
			return provider.Message{}, true, nil, nil
		}
	}

	stopHeartbeat := l.startHeartbeat(callCtx, EventToolCallHeartbeat, label)
	t, out, runErr := func() (tools.Tool, tools.Out, error) {
		defer stopHeartbeat()
		return l.decodeAndRun(callCtx, call, surface)
	}()

	if l.hooksReg != nil {
		_, _ = l.fireHook(callCtx, hooks.PointPostTool, call)
	}

	if runErr != nil {
		if l.onToolError == ErrorPolicyFail {
			return provider.Message{}, false, nil, fmt.Errorf("agentloop: iteration %d: tool call %s: %w", iteration, call.ID, runErr)
		}
		msg, merr := l.toolErrorReportMessage(callCtx, call, iteration, runErr, errorReportContent(runErr))
		if merr != nil {
			return provider.Message{}, false, nil, merr
		}
		return msg, false, runErr, nil
	}

	content, renderErr := l.render(t, out)
	if renderErr != nil {
		if l.onToolError == ErrorPolicyFail {
			return provider.Message{}, false, nil, fmt.Errorf("agentloop: iteration %d: tool call %s: %w", iteration, call.ID, renderErr)
		}
		msg, merr := l.toolErrorReportMessage(callCtx, call, iteration, renderErr, ToolErrorPrefix+renderErr.Error())
		if merr != nil {
			return provider.Message{}, false, nil, merr
		}
		return msg, false, renderErr, nil
	}
	return provider.Message{Role: provider.RoleTool, ToolCallID: call.ID, Name: call.Name, Content: content}, false, nil, nil
}

// toolErrorReportMessage builds the RoleTool message for a reported
// tool-run error, consulting Options.OnToolCallError between the
// policy's report-to-model branch and the default body construction.
// A hook's non-zero Message replaces the default body; a hook error
// fails the call with no append; the zero Message and nil fall
// through to the marked default body.
func (l *Loop) toolErrorReportMessage(ctx context.Context, call provider.ToolCall, iteration int, runErr error, defaultContent string) (provider.Message, error) {
	if l.onToolCallError != nil {
		msg, herr := l.onToolCallError(ctx, call, runErr)
		if herr != nil {
			return provider.Message{}, fmt.Errorf("agentloop: iteration %d: tool call %s: %w", iteration, call.ID, herr)
		}
		if !isZeroMessage(msg) {
			return msg, nil
		}
	}
	return provider.Message{Role: provider.RoleTool, ToolCallID: call.ID, Name: call.Name, Content: defaultContent}, nil
}

// isZeroMessage reports whether m is the zero provider.Message.
func isZeroMessage(m provider.Message) bool {
	return m.Role == "" && m.Content == "" && m.ToolCallID == "" && m.Name == ""
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

// offeredToolNames renders the comma-separated, sorted list of tool names
// actually advertised to the model this iteration (surface.defs - what the
// model was TOLD it could call), for an unknown-tool-name error's corrective
// text. surface.reg is deliberately not used here: Surface's own doc comment
// states Advertised and Registry are independent sets, and defs is what the
// model saw, which is the only list a retry can usefully be told about.
func offeredToolNames(surface runSurface) string {
	if len(surface.defs) == 0 {
		return "(none offered)"
	}
	names := make([]string, len(surface.defs))
	for i, d := range surface.defs {
		names[i] = d.Name
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// decodeAndRun resolves call.Name, checks surface.scope, validates
// call.Arguments against surface.schemas[call.Name], decodes them through
// the resolved tool's DecodeArguments, and calls RunScoped.
func (l *Loop) decodeAndRun(ctx context.Context, call provider.ToolCall, surface runSurface) (tools.Tool, tools.Out, error) {
	t, ok := surface.reg.Get(call.Name)
	if !ok {
		// Name the attempted call and what WAS offered. Without this the
		// model's only signal was the opaque call ID and
		// tools.ErrUnknownName's static text - no attempted name, no valid
		// names - so a model that confabulated or mangled a name had
		// nothing to correct against and kept guessing variants until its
		// step budget ran out.
		return nil, tools.Out{}, fmt.Errorf("agentloop: tool call %s: unknown tool %q (valid tools: %s): %w",
			call.ID, call.Name, offeredToolNames(surface), tools.ErrUnknownName)
	}
	if surface.scope != nil && !surface.scope.Allowed(call.Name, t) {
		return t, tools.Out{}, fmt.Errorf("agentloop: tool call %s: %w", call.ID, tools.ErrScopeDenied)
	}
	st, ok := t.(tools.SchemaTool)
	if !ok {
		return t, tools.Out{}, fmt.Errorf("agentloop: tool %q publishes no schema", call.Name)
	}
	compiled, ok := surface.schemas[call.Name]
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
	out, err := surface.reg.RunScoped(ctx, call.Name, in, surface.scope)
	return t, out, err
}
