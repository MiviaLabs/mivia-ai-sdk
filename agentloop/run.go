package agentloop

import (
	"bytes"
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
// next iteration boundary, with Stop == StopSteered. Final holds the
// zero value, except when Options.StreamingWriter is set: then Final
// carries the bytes the Completer wrote before the steer, the same
// rule every other pre-response graceful stop already follows.
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
	stream := newStreamBuffer(l.streamSink)
	history := append([]provider.Message(nil), msgs...)
	var totalUsage provider.Usage
	var runningTokens int
	iterations := 0
	noticeSent := false
	surface := l.initialSurface()

	for {
		if err := ctx.Err(); err != nil {
			return l.hardFail(history, iterations, totalUsage), err
		}
		// Pull-based injector boundary: drain the installer's messages
		// BEFORE the MaxIterations check, exactly the "before trim"
		// placement the legacy context.go:15-19 use of BeforeStep
		// picked. A nil or empty return is a no-op; a non-empty return
		// grows history by those messages without counting against
		// MaxIterations (the cap is on Completer calls, not on the
		// number of injected frames). A nil steer is the Run
		// (non-steerable) path; the Run contract pre-dates the
		// injector so it never installs one, and we skip the call to
		// avoid a nil-deref on the empty interface.
		if steer != nil {
			if injected := steer.drainInjected(); len(injected) > 0 {
				history = append(history, injected...)
			}
		}
		if iterations >= l.maxIterations {
			return Result{History: history, Iterations: iterations, Usage: totalUsage, Stop: StopMaxIterations}, nil
		}

		// Surface rotation (step 2+): the host hook replaces this
		// iteration's advertised definitions, call-resolution
		// registry, and scope AFTER the injector drain so an
		// injected frame lands on the previous surface, mirroring
		// mivia-agent's legacy applySurfaceHook skip-step-1 rule.
		// A nil return keeps the prior surface; a hook panic fails
		// the run closed rather than half-rotating.
		if l.surfaceFn != nil && iterations >= 1 {
			s, serr := safeSurface(l.surfaceFn)
			if serr != nil {
				return l.hardFail(history, iterations, totalUsage), serr
			}
			var aerr error
			surface, aerr = applySurface(surface, s)
			if aerr != nil {
				return l.hardFail(history, iterations, totalUsage), aerr
			}
		}

		res, err, done := l.runIteration(ctx, &history, &iterations, &totalUsage, &runningTokens, &noticeSent, steer, stream, surface)
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
func (l *Loop) runIteration(ctx context.Context, history *[]provider.Message, iterations *int, totalUsage *provider.Usage, runningTokens *int, noticeSent *bool, steer *Steer, stream *bytes.Buffer, surface runSurface) (res Result, err error, done bool) {
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
		return l.runChat(ctx, *history, *iterations, steer, stream, surface)
	}()
	if at.err != nil {
		if isSteerStop(at.err, ctx, steer, at.fromRecovery) {
			// Steered-stop branch. Two cases:
			//
			// (a) An injector is installed (SetInjector was called).
			//     Case (a) returns IMMEDIATELY after ackTriggered().
			//     The downgrade path does NOT call drainInjected:
			//     the iteration-top boundary at run.go:71-85 drains
			//     the injector on the next loop iteration, so the
			//     deliver-once shape holds across consecutive
			//     payloads. The sticky triggered flag is cleared
			//     here so the next iteration's Chat call arms
			//     un-triggered and proceeds. This mirrors the
			//     legacy requestStep's soft-continue on
			//     errSteerInterrupt so a bridge that polls
			//     continuously across iterations (mivia-agent
			//     bridgeSteerSignals) can deliver repeated steers
			//     within one RunSteerable call without dropping
			//     the run. The drain at the next iteration top
			//     may be a non-empty return (history grows by
			//     those messages) or an empty return (no history
			//     change), and in BOTH cases the loop continues.
			//
			// (b) No injector is installed. The run stops with
			//     StopSteered, the existing single-shot behavior
			//     every pre-injector Steer test pins.
			//
			// The sticky-trigger fix (Part A.4) lives in the
			// case-(a) path: ackTriggered must run BEFORE the next
			// iteration's Chat call arms, otherwise arm sees
			// triggered=true and cancels instantly, the next drain
			// is empty, and the run spins between two empty drains.
			if steer.hasInjector() {
				steer.ackTriggered()
				return Result{}, nil, false
			}
			return steeredStopResult(*history, *iterations, *totalUsage, stream), nil, true
		}
		if at.fromRecovery {
			return Result{History: *history, Iterations: *iterations, Usage: *totalUsage}, at.err, true
		}
		return l.hardFail(*history, *iterations, *totalUsage), at.err, true
	}
	return l.afterChat(ctx, at, history, iterations, totalUsage, runningTokens, noticeInRequest, surface)
}

// afterChat holds the second half of one iteration's body: recording
// the response, audit, the token-budget check, and tool-call dispatch
// through runToolStage. Split from runIteration to keep both under
// the structure gate's per-function line cap. noticeInRequest,
// computed by runIteration, picks StopConcluded over StopNoToolCalls
// when this iteration's Completer request carried the ConcludeMargin
// nudge.
func (l *Loop) afterChat(ctx context.Context, at chatAttempt, history *[]provider.Message, iterations *int, totalUsage *provider.Usage, runningTokens *int, noticeInRequest bool, surface runSurface) (Result, error, bool) {
	resp, req := at.resp, at.req
	*history = at.history
	*history = append(*history, resp.Message)
	*iterations++
	*totalUsage = sumUsage(*totalUsage, resp.Usage)
	if l.usageAcc != nil {
		_ = l.usageAcc.Record(l.sessionID, resp.Usage)
	}
	if resp.Message.Role == provider.RoleAssistant {
		l.emitEvent(ctx, EventAssistant, resp.Message.Content)
	}
	l.emitThinkingEvents(ctx, resp.Message.ReasoningContent)
	if resp.CacheUsage.Reported {
		if data, ok := encodeEventData(resp.CacheUsage); ok {
			l.emitEvent(ctx, EventCacheUsage, data)
		}
	}
	if l.audit != nil {
		if aerr := l.audit(ctx, AuditRecord{
			Iteration:       *iterations,
			Kind:            AuditKindCompletion,
			Request:         req,
			Response:        resp,
			ThinkingContent: resp.Message.ReasoningContent,
			CacheUsage:      resp.CacheUsage,
		}); aerr != nil {
			return l.hardFail(*history, *iterations, *totalUsage),
				fmt.Errorf("agentloop: iteration %d: audit: %w", *iterations, aerr), true
		}
	}
	if l.calibrated != nil {
		l.calibrated.Observe(at.estimatedTokens, resp.Usage.TotalTokens)
		if data, ok := encodeEventData(calibrationPayload{Estimated: at.estimatedTokens, Actual: resp.Usage.TotalTokens}); ok {
			l.emitEvent(ctx, EventCalibrationDelta, data)
		}
	}
	*runningTokens += billedTokens(resp.Usage)
	if l.maxTotalTokens > 0 && *runningTokens > l.maxTotalTokens {
		return l.hardFail(*history, *iterations, *totalUsage),
			fmt.Errorf("agentloop: iteration %d: %w", *iterations, ErrTokenBudgetExceeded), true
	}

	res, done, terr := l.runToolStage(at.iterCtx, *history, resp, *iterations, *totalUsage, noticeInRequest, surface)
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
func (l *Loop) runToolStage(ctx context.Context, history []provider.Message, resp provider.Response, iterations int, totalUsage provider.Usage, noticeInRequest bool, surface runSurface) (Result, bool, error) {
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

	newHistory, veto, err := l.runToolCalls(ctx, history, resp.ToolCalls, iterations, surface)
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
// not already canceled ahead of the call, and steer's wasTriggered
// reports true.
func isSteerStop(err error, ctx context.Context, steer *Steer, fromRecovery bool) bool {
	if steer == nil || fromRecovery || ctx.Err() != nil {
		return false
	}
	if !errors.Is(err, context.Canceled) {
		return false
	}
	return steer.wasTriggered()
}

// noticePresent and shouldConclude live in conclude.go alongside
// the other conclude helpers. Splitting them out keeps run.go under
// the structure gate's per-file line cap.

// chatAttempt carries one iteration's Completer outcome. iterCtx is
// the span-annotated context the iteration ran under, for the turn's
// subsequent tool calls. fromRecovery distinguishes an
// ErrPromptTooLong-recovered completion from a primary one so the
// error branch can skip the isSteerStop downgrade for a steer that
// arrived mid-recovery. estimatedTokens is what Calibrated.EstimateTokens
// returned for this iteration's request, before Chat ran.
type chatAttempt struct {
	resp            provider.Response
	req             provider.Request
	history         []provider.Message
	iterCtx         context.Context
	err             error
	fromRecovery    bool
	estimatedTokens int
}

// runChat builds this iteration's Request, reserves budget, calls
// Completer.Chat, and catches ErrPromptTooLong to attempt one-shot
// compaction recovery when Options.Window is set.
func (l *Loop) runChat(ctx context.Context, history []provider.Message, iterations int, steer *Steer, stream *bytes.Buffer, surface runSurface) chatAttempt {
	var span *trace.Span
	if l.tracer != nil {
		ctx, span = l.tracer.Start(ctx, "agentloop.iteration")
	}
	req := provider.Request{Model: l.model, Messages: history, Tools: surface.defs}
	req.StreamingWriter = streamMirror(l.streamSink, stream)
	estimated := l.estimateTokens(req)
	// WorkBudget call points (agentloop/budget.go): reserve before the
	// call on the exact request (a reserve error fails the run before
	// the call runs), refund the never-consumed reservation when the
	// call fails, settle with the real Usage when it succeeds.
	if rerr := l.reserveWork(ctx, req, iterations+1); rerr != nil {
		return chatAttempt{err: rerr, iterCtx: ctx}
	}
	resp, err := l.steerableChat(ctx, req, steer)
	if span != nil {
		span.End()
	}
	if err == nil {
		l.settleWork(ctx, req, resp.Usage)
		return chatAttempt{resp: resp, req: req, history: history, iterCtx: ctx,
			estimatedTokens: estimated}
	}
	l.refundWork(ctx, req)
	if l.window == nil || !errors.Is(err, provider.ErrPromptTooLong) {
		return chatAttempt{err: err, iterCtx: ctx}
	}
	recovered, rebuilt, retryReq, rerr := l.recoverPromptTooLong(ctx, err, history, iterations, surface)
	if rerr != nil {
		return chatAttempt{err: rerr, fromRecovery: true, iterCtx: ctx}
	}
	l.settleWork(ctx, retryReq, recovered.Usage)
	return chatAttempt{resp: recovered, req: retryReq, history: rebuilt, iterCtx: ctx,
		estimatedTokens: l.estimateTokens(retryReq)}
}

// estimateTokens returns l.calibrated.EstimateTokens(req), or zero
// when l.calibrated is nil or the estimate call fails. Zero is a
// safe default: Calibrated.Observe no-ops on a non-positive estimated
// value, so an estimator failure here degrades silently, the same
// rule EstimateTokens failures already followed outside planning.
func (l *Loop) estimateTokens(req provider.Request) int {
	if l.calibrated == nil {
		return 0
	}
	est, err := l.calibrated.EstimateTokens(req)
	if err != nil {
		return 0
	}
	return est
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
