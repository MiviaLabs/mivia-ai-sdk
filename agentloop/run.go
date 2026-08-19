package agentloop

import (
	"context"
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

		iterCtx := ctx
		var span *trace.Span
		if l.tracer != nil {
			iterCtx, span = l.tracer.Start(ctx, "agentloop.iteration")
		}
		resp, err := l.completer.Chat(iterCtx, provider.Request{Model: l.model, Messages: history, Tools: l.defs})
		if span != nil {
			span.End()
		}
		if err != nil {
			return l.hardFail(history, iterations, totalUsage), err
		}

		history = append(history, resp.Message)
		iterations++
		totalUsage = sumUsage(totalUsage, resp.Usage)
		if l.usageAcc != nil {
			_ = l.usageAcc.Record(l.sessionID, resp.Usage)
		}
		runningTokens += resp.Usage.TotalTokens
		if l.maxTotalTokens > 0 && runningTokens > l.maxTotalTokens {
			return l.hardFail(history, iterations, totalUsage),
				fmt.Errorf("agentloop: iteration %d: %w", iterations, ErrTokenBudgetExceeded)
		}

		if len(resp.ToolCalls) == 0 {
			return Result{Final: resp.Message, History: history, Iterations: iterations, Usage: totalUsage, Stop: StopNoToolCalls}, nil
		}
		if l.maxCallsPerTurn > 0 && len(resp.ToolCalls) > l.maxCallsPerTurn {
			return l.hardFail(history, iterations, totalUsage),
				fmt.Errorf("agentloop: iteration %d: %w", iterations, ErrCallsPerTurnExceeded)
		}

		newHistory, veto, err := l.runToolCalls(iterCtx, history, resp.ToolCalls, iterations)
		history = newHistory
		if err != nil {
			return l.hardFail(history, iterations, totalUsage), err
		}
		if veto {
			return Result{History: history, Iterations: iterations, Usage: totalUsage, Stop: StopHookVeto}, nil
		}
	}
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
