package agentloop

import (
	"context"
	"errors"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/trace"
)

// Run calls Registry.RunScoped, never Registry.Run, so a model-chosen
// call always passes through l.scope. See docs/plans/agentloop.md for
// the full termination and Result-shape contract. A wired Hooks
// registry fires PointStop exactly once, on every return path, with
// the returned Result as payload; a wired handler's veto or error
// never changes what Run already decided to return. See RunSteerable
// for a graceful, in-flight stop a caller can request mid-run.
func (l *Loop) Run(ctx context.Context, msgs []provider.Message) (Result, error) {
	return l.RunSteerable(ctx, msgs, nil)
}

// RunSteerable is Run with one addition: a non-nil steer lets the
// caller request a soft-cancel of the current iteration's in-flight
// Completer.Chat call from another goroutine, through steer.Trigger.
// ctx cancellation still ends the run as a hard failure, unchanged
// from Run. A triggered steer ends the run gracefully instead, at the
// next iteration boundary, with Stop == StopSteered and Final holding
// the zero value: the run stops before a new response arrives, the
// same rule every other pre-response graceful stop already follows.
// History, Iterations, and Usage carry every already-completed
// iteration's state. Run(ctx, msgs) is equivalent to
// RunSteerable(ctx, msgs, nil).
func (l *Loop) RunSteerable(ctx context.Context, msgs []provider.Message, steer *Steer) (Result, error) {
	if steer != nil {
		steer.reset()
	}
	res, err := l.run(ctx, msgs, steer)
	l.fireStop(ctx, res)
	return res, err
}

// fireStop fires PointStop with res as payload when l.hooksReg is
// wired. Its return is informational: the loop has already decided
// its stop, so a veto or handler error changes nothing Run returns.
func (l *Loop) fireStop(ctx context.Context, res Result) {
	if l.hooksReg == nil {
		return
	}
	_ = l.hooksReg.Fire(ctx, hooks.PointStop, res)
}

// run is Run's loop body, run once per Run or RunSteerable call. It
// holds only loop control: the ctx-cancellation check, the
// MaxIterations check, one call to runIteration, and the
// iteration-done decision that call returns. runIteration holds the
// single-iteration body. A nil steer behaves exactly as before
// RunSteerable existed.
func (l *Loop) run(ctx context.Context, msgs []provider.Message, steer *Steer) (Result, error) {
	history := append([]provider.Message(nil), msgs...)
	var totalUsage provider.Usage
	var runningTokens int
	iterations := 0
	noticeSent := false

	for {
		if err := ctx.Err(); err != nil {
			return l.hardFail(history, iterations, totalUsage), err
		}
		if iterations >= l.maxIterations {
			return Result{History: history, Iterations: iterations, Usage: totalUsage, Stop: StopMaxIterations}, nil
		}

		res, err, done := l.runIteration(ctx, &history, &iterations, &totalUsage, &runningTokens, &noticeSent, steer)
		if done {
			return res, err
		}
	}
}

// runIteration runs one loop iteration: trim, window plan, the
// token-budget check, the ConcludeMargin nudge, the Completer call,
// audit, the token-budget check, and tool-call dispatch. It mutates
// history, iterations, totalUsage, runningTokens, and noticeSent in
// place through their pointers, matching run's prior inlined
// mutation. done reports whether run must return (res, err) now; done
// false means the iteration completed normally and run should loop
// again. EventIterationStart fires on entry; EventIterationEnd fires
// from a deferred closure covering every exit path, so every
// hard-fail cause and every graceful stop emits it exactly once.
// checkBudget runs after planHistory: window-based compaction can
// bring an over-budget history back under budget, so the check must
// see the post-compaction history, not the pre-compaction one.
func (l *Loop) runIteration(ctx context.Context, history *[]provider.Message, iterations *int, totalUsage *provider.Usage, runningTokens *int, noticeSent *bool, steer *Steer) (res Result, err error, done bool) {
	label := iterationLabel(*iterations + 1)
	l.emitEvent(ctx, EventIterationStart, label)
	defer func() { l.emitEvent(ctx, EventIterationEnd, label) }()

	trimmed, terr := l.applyTrim(ctx, *history, *iterations)
	if terr != nil {
		return l.hardFail(*history, *iterations, *totalUsage), terr, true
	}
	*history = trimmed

	if l.window != nil {
		planned, perr := l.planHistory(ctx, *history, *iterations)
		if perr != nil {
			return l.hardFail(*history, *iterations, *totalUsage), perr, true
		}
		*history = planned
	}

	if berr := l.checkBudget(*history, *iterations); berr != nil {
		return l.hardFail(*history, *iterations, *totalUsage), berr, true
	}

	if !*noticeSent && l.shouldConclude(*iterations) {
		*history = append(*history, provider.Message{Role: provider.RoleUser, Content: l.concludeNotice})
		*noticeSent = true
	}
	// noticeSent gates noticePresent: this run's own nudge must fire.
	noticeInRequest := *noticeSent && noticePresent(*history, l.concludeNotice)

	stopHeartbeat := l.startHeartbeat(ctx, EventCompletionHeartbeat, label)
	at := func() chatAttempt {
		defer stopHeartbeat()
		return l.runChat(ctx, *history, *iterations, steer)
	}()
	if at.err != nil {
		if isSteerStop(at.err, ctx, steer, at.fromRecovery) {
			return Result{History: *history, Iterations: *iterations, Usage: *totalUsage, Stop: StopSteered}, nil, true
		}
		if at.fromRecovery {
			return Result{History: *history, Iterations: *iterations, Usage: *totalUsage}, at.err, true
		}
		return l.hardFail(*history, *iterations, *totalUsage), at.err, true
	}
	return l.afterChat(ctx, at, history, iterations, totalUsage, runningTokens, noticeInRequest)
}

// afterChat holds the second half of one iteration's body: recording
// the response, audit, the token-budget check, and tool-call dispatch
// through runToolStage. Split from runIteration to keep both under
// the structure gate's per-function line cap. noticeInRequest,
// computed by runIteration, picks StopConcluded over StopNoToolCalls
// when this iteration's Completer request carried the ConcludeMargin
// nudge.
func (l *Loop) afterChat(ctx context.Context, at chatAttempt, history *[]provider.Message, iterations *int, totalUsage *provider.Usage, runningTokens *int, noticeInRequest bool) (Result, error, bool) {
	resp, req := at.resp, at.req
	*history = at.history
	*history = append(*history, resp.Message)
	*iterations++
	*totalUsage = sumUsage(*totalUsage, resp.Usage)
	if l.usageAcc != nil {
		_ = l.usageAcc.Record(l.sessionID, resp.Usage)
	}
	if l.audit != nil {
		if aerr := l.audit(ctx, AuditRecord{Iteration: *iterations, Kind: AuditKindCompletion, Request: req, Response: resp}); aerr != nil {
			return l.hardFail(*history, *iterations, *totalUsage),
				fmt.Errorf("agentloop: iteration %d: audit: %w", *iterations, aerr), true
		}
	}
	if l.calibrated != nil {
		l.calibrated.Observe(resp.Usage.TotalTokens)
	}
	*runningTokens += billedTokens(resp.Usage)
	if l.maxTotalTokens > 0 && *runningTokens > l.maxTotalTokens {
		return l.hardFail(*history, *iterations, *totalUsage),
			fmt.Errorf("agentloop: iteration %d: %w", *iterations, ErrTokenBudgetExceeded), true
	}

	res, done, terr := l.runToolStage(at.iterCtx, *history, resp, *iterations, *totalUsage, noticeInRequest)
	if done {
		return res, terr, true
	}
	*history = res.History
	return Result{}, nil, false
}

// runToolStage dispatches resp's tool calls, or reports StopNoToolCalls
// (or StopConcluded, when noticeInRequest holds) when resp carries
// none. The returned bool reports whether the caller must return res
// (and err) as the iteration's own result; when false, res.History
// carries the loop's next history and the loop continues.
func (l *Loop) runToolStage(ctx context.Context, history []provider.Message, resp provider.Response, iterations int, totalUsage provider.Usage, noticeInRequest bool) (Result, bool, error) {
	if len(resp.ToolCalls) == 0 {
		stop := StopNoToolCalls
		if noticeInRequest {
			stop = StopConcluded
		}
		return Result{Final: resp.Message, History: history, Iterations: iterations, Usage: totalUsage, Stop: stop}, true, nil
	}
	if l.maxCallsPerTurn > 0 && len(resp.ToolCalls) > l.maxCallsPerTurn {
		return l.hardFail(history, iterations, totalUsage), true,
			fmt.Errorf("agentloop: iteration %d: %w", iterations, ErrCallsPerTurnExceeded)
	}

	newHistory, veto, err := l.runToolCalls(ctx, history, resp.ToolCalls, iterations)
	if err != nil {
		return l.hardFail(newHistory, iterations, totalUsage), true, err
	}
	if veto {
		return Result{History: newHistory, Iterations: iterations, Usage: totalUsage, Stop: StopHookVeto}, true, nil
	}
	return Result{History: newHistory}, false, nil
}

// isSteerStop reports whether err is runChat's Completer.Chat call
// reacting to steer's Trigger-derived cancellation, rather than a
// hard failure. All four conditions must hold: steer is non-nil, err
// did not come from the prompt-too-long recovery path, ctx itself was
// not directly canceled, and err wraps context.Canceled while
// steer.wasTriggered reports true. wasTriggered rules out a Completer
// that independently wraps context.Canceled while a Steer happens to
// be present but was never triggered. The ctx.Err() check tells a
// steer-triggered cancellation of runChat's derived context apart
// from a caller's direct ctx cancellation, which also propagates into
// that derived context since context.WithCancel's child observes its
// parent.
func isSteerStop(err error, ctx context.Context, steer *Steer, fromRecovery bool) bool {
	if steer == nil || fromRecovery {
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	if !errors.Is(err, context.Canceled) {
		return false
	}
	return steer.wasTriggered()
}

// noticePresent reports whether history carries the ConcludeNotice
// among its messages, the present-tense signal for whether the model
// actually saw the nudge in this iteration's Completer request. A
// sticky "was the notice ever appended" flag is not enough:
// Options.Trim (or Window) may strip the notice out of a later
// iteration's history before that iteration's Completer call runs.
// See docs/plans/agents/phase79_graceful_conclude.md's Trim limit.
// Callers must also gate this on noticeSent: noticePresent alone
// cannot tell this run's own nudge apart from matching text a caller
// fed in through the starting History for an unrelated reason.
func noticePresent(history []provider.Message, notice string) bool {
	for _, m := range history {
		if m.Role == provider.RoleUser && m.Content == notice {
			return true
		}
	}
	return false
}

// shouldConclude reports whether the upcoming Completer call, the
// 1-based iteration k = iterations+1, qualifies for the ConcludeMargin
// nudge: MaxIterations-k < ConcludeMargin. A zero ConcludeMargin never
// qualifies, since k never exceeds MaxIterations. See
// docs/plans/agents/phase79_graceful_conclude.md for the worked table.
func (l *Loop) shouldConclude(iterations int) bool {
	if l.concludeMargin <= 0 {
		return false
	}
	k := iterations + 1
	return l.maxIterations-k < l.concludeMargin
}

// chatAttempt carries one iteration's Completer outcome. iterCtx is
// the span-annotated context the iteration ran under, for the turn's
// later tool calls. When err is set, fromRecovery distinguishes the
// recovery path's carrying Result rule from the base hard-fail rule.
type chatAttempt struct {
	resp         provider.Response
	req          provider.Request
	history      []provider.Message
	iterCtx      context.Context
	err          error
	fromRecovery bool
}

// runChat performs one iteration's Completer call under the iteration
// span, recovering exactly once from a prompt-too-long rejection when
// a window is set. A non-nil steer derives a per-call context, arms
// steer with that context's cancel func before the call, and disarms
// it after, so Trigger can soft-cancel this one Completer.Chat call
// without canceling ctx itself. A steer already triggered before this
// call arms fires the derived cancel immediately, before
// l.completer.Chat runs, carrying a Trigger fired during the previous
// iteration's tool-call batch into this call's start. The returned
// chatAttempt.iterCtx always carries ctx, never the derived context: a
// later tool call in this turn must not run under a context a
// completed Completer.Chat call already canceled. recoverPromptTooLong
// keeps using ctx too, so a Trigger fired during that retry has no
// effect until the next iteration boundary.
func (l *Loop) runChat(ctx context.Context, history []provider.Message, iterations int, steer *Steer) chatAttempt {
	var span *trace.Span
	if l.tracer != nil {
		ctx, span = l.tracer.Start(ctx, "agentloop.iteration")
	}
	req := provider.Request{Model: l.model, Messages: history, Tools: l.defs}
	resp, err := l.steerableChat(ctx, req, steer)
	if span != nil {
		span.End()
	}
	if err == nil {
		return chatAttempt{resp: resp, req: req, history: history, iterCtx: ctx}
	}
	if l.window == nil || !errors.Is(err, provider.ErrPromptTooLong) {
		return chatAttempt{err: err, iterCtx: ctx}
	}
	recovered, rebuilt, retryReq, rerr := l.recoverPromptTooLong(ctx, err, history, iterations)
	if rerr != nil {
		return chatAttempt{err: rerr, fromRecovery: true, iterCtx: ctx}
	}
	return chatAttempt{resp: recovered, req: retryReq, history: rebuilt, iterCtx: ctx}
}

// steerableChat calls l.completer.Chat on ctx directly when steer is
// nil. When steer is non-nil, it derives a child context, arms steer
// with the child's cancel func, calls Chat on the child, then disarms
// steer and cancels the child before returning: no derived context
// leaks past this one call.
func (l *Loop) steerableChat(ctx context.Context, req provider.Request, steer *Steer) (provider.Response, error) {
	if steer == nil {
		return l.completer.Chat(ctx, req)
	}
	chatCtx, cancel := context.WithCancel(ctx)
	if steer.arm(cancel) {
		cancel()
	}
	defer steer.disarm()
	defer cancel()
	return l.completer.Chat(chatCtx, req)
}

// hardFail builds the Result a hard-fail error return carries. Every
// hard-fail cause shares one rule: when at least one iteration has
// already completed, the partial History, Iterations, and Usage
// travel with the error. When none has, no partial state exists yet,
// and the rule degrades to the zero-value Result on its own, with no
// special case for ctx cancellation or any other cause.
func (l *Loop) hardFail(history []provider.Message, iterations int, totalUsage provider.Usage) Result {
	if iterations == 0 {
		return Result{}
	}
	return Result{History: history, Iterations: iterations, Usage: totalUsage}
}

// billedTokens returns the larger, more trustworthy reading of one
// response's token cost: the reported TotalTokens, or the sum of
// PromptTokens and CompletionTokens, whichever is greater. provider.Usage
// enforces no relationship between its fields, so a Completer that leaves
// TotalTokens at zero must not silently bypass MaxTotalTokens.
func billedTokens(u provider.Usage) int {
	sum := u.PromptTokens + u.CompletionTokens
	if u.TotalTokens > sum {
		return u.TotalTokens
	}
	return sum
}

// sumUsage adds b's four fields onto a and returns the sum.
func sumUsage(a, b provider.Usage) provider.Usage {
	return provider.Usage{
		PromptTokens:     a.PromptTokens + b.PromptTokens,
		CompletionTokens: a.CompletionTokens + b.CompletionTokens,
		TotalTokens:      a.TotalTokens + b.TotalTokens,
		CachedTokens:     a.CachedTokens + b.CachedTokens,
	}
}

// applyTrim runs l.trim on history when set, then validates every
// message in the result. A nil l.trim passes history through
// unchanged and skips validation.
func (l *Loop) applyTrim(ctx context.Context, history []provider.Message, iteration int) ([]provider.Message, error) {
	if l.trim == nil {
		return history, nil
	}
	trimmed, err := l.trim(ctx, history)
	if err != nil {
		return nil, fmt.Errorf("agentloop: iteration %d: trim: %w", iteration, err)
	}
	for _, m := range trimmed {
		if err := m.Validate(); err != nil {
			return nil, fmt.Errorf("agentloop: iteration %d: trimmed message: %w", iteration, err)
		}
	}
	return trimmed, nil
}

// checkBudget sums history's content bytes and message count and
// checks them against l.budget.Fits. A nil l.budget means uncapped.
func (l *Loop) checkBudget(history []provider.Message, iteration int) error {
	if l.budget == nil {
		return nil
	}
	var bytes int
	for _, m := range history {
		bytes += len(m.Content)
	}
	if !l.budget.Fits(bytes, len(history)) {
		return fmt.Errorf("agentloop: iteration %d: %w", iteration, ErrOverBudget)
	}
	return nil
}
