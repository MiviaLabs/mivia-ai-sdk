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
// never changes what Run already decided to return.
func (l *Loop) Run(ctx context.Context, msgs []provider.Message) (Result, error) {
	res, err := l.run(ctx, msgs)
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

// run is Run's loop body, run once per Run call.
func (l *Loop) run(ctx context.Context, msgs []provider.Message) (Result, error) {
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

		trimmed, err := l.applyTrim(ctx, history, iterations)
		if err != nil {
			return l.hardFail(history, iterations, totalUsage), err
		}
		history = trimmed

		if err := l.checkBudget(history, iterations); err != nil {
			return l.hardFail(history, iterations, totalUsage), err
		}

		if l.window != nil {
			planned, err := l.planHistory(ctx, history, iterations)
			if err != nil {
				return Result{History: history, Iterations: iterations, Usage: totalUsage}, err
			}
			history = planned
		}

		if !noticeSent && l.shouldConclude(iterations) {
			history = append(history, provider.Message{Role: provider.RoleUser, Content: l.concludeNotice})
			noticeSent = true
		}
		// noticeSent gates noticePresent: this run's own nudge must fire.
		noticeInRequest := noticeSent && noticePresent(history, l.concludeNotice)

		at := l.runChat(ctx, history, iterations)
		if at.err != nil {
			if at.fromRecovery {
				return Result{History: history, Iterations: iterations, Usage: totalUsage}, at.err
			}
			return l.hardFail(history, iterations, totalUsage), at.err
		}
		resp, req := at.resp, at.req
		history = at.history
		history = append(history, resp.Message)
		iterations++
		var bookErr error
		totalUsage, runningTokens, bookErr = l.recordChat(ctx, iterations, req, resp, totalUsage, runningTokens)
		if bookErr != nil {
			return l.hardFail(history, iterations, totalUsage), bookErr
		}

		if len(resp.ToolCalls) == 0 {
			stop := StopNoToolCalls
			if noticeInRequest {
				stop = StopConcluded
			}
			return Result{Final: resp.Message, History: history, Iterations: iterations, Usage: totalUsage, Stop: stop}, nil
		}
		if l.maxCallsPerTurn > 0 && len(resp.ToolCalls) > l.maxCallsPerTurn {
			return l.hardFail(history, iterations, totalUsage),
				fmt.Errorf("agentloop: iteration %d: %w", iterations, ErrCallsPerTurnExceeded)
		}

		newHistory, veto, err := l.runToolCalls(at.iterCtx, history, resp.ToolCalls, iterations)
		history = newHistory
		if err != nil {
			return l.hardFail(history, iterations, totalUsage), err
		}
		if veto {
			return Result{History: history, Iterations: iterations, Usage: totalUsage, Stop: StopHookVeto}, nil
		}
	}
}

// recordChat folds one completed iteration's usage, audit, and
// token-budget bookkeeping: sums usage, records it, fires Audit,
// observes the estimator, and checks MaxTotalTokens. Returns the
// updated totalUsage and runningTokens, and the first error among
// audit and the token-budget check.
func (l *Loop) recordChat(ctx context.Context, iterations int, req provider.Request, resp provider.Response, totalUsage provider.Usage, runningTokens int) (provider.Usage, int, error) {
	totalUsage = sumUsage(totalUsage, resp.Usage)
	if l.usageAcc != nil {
		_ = l.usageAcc.Record(l.sessionID, resp.Usage)
	}
	if l.audit != nil {
		if err := l.audit(ctx, AuditRecord{Iteration: iterations, Kind: AuditKindCompletion, Request: req, Response: resp}); err != nil {
			return totalUsage, runningTokens, fmt.Errorf("agentloop: iteration %d: audit: %w", iterations, err)
		}
	}
	if l.calibrated != nil {
		l.calibrated.Observe(resp.Usage.TotalTokens)
	}
	runningTokens += billedTokens(resp.Usage)
	if l.maxTotalTokens > 0 && runningTokens > l.maxTotalTokens {
		return totalUsage, runningTokens, fmt.Errorf("agentloop: iteration %d: %w", iterations, ErrTokenBudgetExceeded)
	}
	return totalUsage, runningTokens, nil
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
// a window is set.
func (l *Loop) runChat(ctx context.Context, history []provider.Message, iterations int) chatAttempt {
	var span *trace.Span
	if l.tracer != nil {
		ctx, span = l.tracer.Start(ctx, "agentloop.iteration")
	}
	req := provider.Request{Model: l.model, Messages: history, Tools: l.defs}
	resp, err := l.completer.Chat(ctx, req)
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
