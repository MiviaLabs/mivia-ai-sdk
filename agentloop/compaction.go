package agentloop

import (
	"context"
	"errors"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/context/plan"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// planHistory runs the per-iteration planning step: estimate the
// history, pass through under the trigger, compact above it. An
// estimate error fails the iteration with ErrPlanFailed.
func (l *Loop) planHistory(ctx context.Context, history []provider.Message, iteration int) ([]provider.Message, error) {
	est, err := l.calibrated.EstimateTokens(provider.Request{Messages: history})
	if err != nil {
		return nil, fmt.Errorf("agentloop: iteration %d: %w: %w", iteration, ErrPlanFailed, err)
	}
	if est < l.window.CompactTrigger() {
		return history, nil
	}
	rebuilt, _, err := l.compactHistory(ctx, history, *l.window, iteration, false)
	return rebuilt, err
}

// compactHistory runs the compaction sequence over history under w.
// It returns the rebuilt history and whether Compact compacted
// anything. notice appends one CompactionNotice message after the
// summary injection, the recovery path's addition; under notice, an
// uncompacted result returns early and unchanged, so the caller can
// treat it as unrecoverable. A summarizer skip reuses the prior
// summary when one was held aside; with no prior, the planning path
// proceeds without a summary and the recovery path returns an
// uncompacted nil history. Every path passes checkCompactedBudget
// before returning a rebuilt history. A failure returns a nil
// history; history itself never changes on failure.
func (l *Loop) compactHistory(ctx context.Context, history []provider.Message, w plan.Window, iteration int, notice bool) ([]provider.Message, bool, error) {
	adjusted := preserveSummaryName(w)
	prior, rest := splitSummary(history)
	res, err := plan.Compact(rest, adjusted, l.calibrated)
	if err != nil {
		return nil, false, fmt.Errorf("agentloop: iteration %d: %w: %w", iteration, ErrCompactionFailed, err)
	}
	if !res.Compacted && notice {
		return nil, false, nil
	}
	rebuilt := append([]provider.Message(nil), res.Kept...)
	injected := false
	if len(res.Dropped) > 0 || prior != nil {
		summary, skipped, err := l.summarizeDropped(ctx, prior, res.Dropped)
		if err != nil {
			return nil, false, err
		}
		switch {
		case !skipped:
			rebuilt = injectAfterSystem(rebuilt, plan.SummaryMessage(summary))
			injected = true
		case prior != nil:
			// Skip with a prior: re-inject the prior summary
			// unchanged. It keeps SummaryMessageName, so a
			// recovery notice lands directly after it.
			rebuilt = injectAfterSystem(rebuilt, *prior)
			injected = true
		case notice:
			// Skip with no prior, recovery path: no summary to
			// reuse, so a retry would resend the same oversized
			// prompt. Unrecoverable, like the !compacted case.
			return nil, false, nil
		}
		// Skip with no prior, planning path: inject nothing. The
		// dropped messages stay dropped and the run proceeds with
		// the kept history.
	}
	if notice {
		rebuilt = injectNotice(rebuilt, injected)
	}
	if err := l.checkCompactedBudget(rebuilt, w, iteration); err != nil {
		return nil, false, err
	}
	return rebuilt, res.Compacted, nil
}

// summarizeDropped prepends the held-aside prior summary, when one
// exists, to the dropped messages and runs one summarizer call. The
// bool reports a skip: the summarizer returned
// plan.ErrSummarySkipped, so no summary exists and the
// caller reuses the prior or proceeds without one. Any other error
// keeps the ErrCompactionFailed wrap.
func (l *Loop) summarizeDropped(ctx context.Context, prior *provider.Message, dropped []provider.Message) (plan.Summary, bool, error) {
	input := dropped
	if prior != nil {
		input = append([]provider.Message{*prior}, dropped...)
	}
	summary, err := l.summarizer.Summarize(ctx, input)
	if errors.Is(err, plan.ErrSummarySkipped) {
		return plan.Summary{}, true, nil
	}
	if err != nil {
		return plan.Summary{}, false, fmt.Errorf("agentloop: %w: %w", ErrCompactionFailed, err)
	}
	return summary, false, nil
}

// checkCompactedBudget re-estimates the rebuilt history and fails
// closed above the effective window's budget.
func (l *Loop) checkCompactedBudget(rebuilt []provider.Message, w plan.Window, iteration int) error {
	est, err := l.calibrated.EstimateTokens(provider.Request{Messages: rebuilt})
	if err != nil {
		return fmt.Errorf("agentloop: iteration %d: %w: %w", iteration, ErrCompactionFailed, err)
	}
	if est > w.Budget() {
		return fmt.Errorf("agentloop: iteration %d: %w: %w", iteration, ErrCompactionFailed, plan.ErrRetentionOverflow)
	}
	return nil
}

// recoveryWindow builds the prompt-too-long recovery window: the
// caller's window with the trigger at one percent and the target at
// max(1, min(RecoveryTargetTokens, Budget over four)).
func recoveryWindow(w plan.Window) plan.Window {
	rw := w
	rw.Compaction.TriggerPercent = 1
	rw.Compaction.TargetTokens = max(1, min(RecoveryTargetTokens, w.Budget()/4))
	return rw
}

// preserveSummaryName copies w and appends the summary message name
// to the copy's PreserveNames only when absent, into a freshly
// allocated slice. The caller's backing array never changes.
func preserveSummaryName(w plan.Window) plan.Window {
	for _, name := range w.Compaction.PreserveNames {
		if name == plan.SummaryMessageName {
			return w
		}
	}
	fresh := make([]string, 0, len(w.Compaction.PreserveNames)+1)
	fresh = append(fresh, w.Compaction.PreserveNames...)
	w.Compaction.PreserveNames = append(fresh, plan.SummaryMessageName)
	return w
}

// splitSummary removes every message named SummaryMessageName from
// msgs and returns the first one held aside, plus the rest.
func splitSummary(msgs []provider.Message) (*provider.Message, []provider.Message) {
	var prior *provider.Message
	rest := make([]provider.Message, 0, len(msgs))
	for i := range msgs {
		if msgs[i].Name == plan.SummaryMessageName {
			if prior == nil {
				prior = &msgs[i]
			}
			continue
		}
		rest = append(rest, msgs[i])
	}
	return prior, rest
}

// injectAfterSystem inserts msg directly after the leading system
// message, or at index zero when none leads. msgs is never empty:
// Compact always keeps the mandatory retention set, which holds at
// least the user objective.
func injectAfterSystem(msgs []provider.Message, msg provider.Message) []provider.Message {
	out := make([]provider.Message, 0, len(msgs)+1)
	if msgs[0].Role == provider.RoleSystem {
		out = append(out, msgs[0], msg)
		return append(out, msgs[1:]...)
	}
	out = append(out, msg)
	return append(out, msgs...)
}

// injectNotice appends the CompactionNotice message directly after
// the summary injection: after the summary message when one was
// injected, else at the injection point.
func injectNotice(msgs []provider.Message, summaryInjected bool) []provider.Message {
	if !summaryInjected {
		return injectAfterSystem(msgs, provider.Message{Role: provider.RoleUser, Content: CompactionNotice})
	}
	out := make([]provider.Message, 0, len(msgs)+1)
	for i := range msgs {
		out = append(out, msgs[i])
		if msgs[i].Name == plan.SummaryMessageName {
			out = append(out, provider.Message{Role: provider.RoleUser, Content: CompactionNotice})
		}
	}
	return out
}

// recoverPromptTooLong handles one ErrPromptTooLong rejection: compact
// under the recovery window, append the notice, and retry the same
// iteration's Chat exactly once. When the compaction sequence returns
// Compacted false, the history estimates under one percent of the
// budget, so orig returns unchanged with no retry and no notice. Any
// retry error, including a second rejection, propagates. An invalid
// recovery window (a Budget of one token) fails closed inside
// compactHistory's own plan.Compact call, wrapping the same
// window error recoverPromptTooLong would have; no separate check is
// needed here.
func (l *Loop) recoverPromptTooLong(ctx context.Context, orig error, history []provider.Message, iteration int, surface runSurface) (provider.Response, []provider.Message, provider.Request, error) {
	rw := recoveryWindow(*l.window)
	rebuilt, compacted, err := l.compactHistory(ctx, history, rw, iteration, true)
	if err != nil {
		return provider.Response{}, nil, provider.Request{}, err
	}
	if !compacted {
		return provider.Response{}, nil, provider.Request{}, orig
	}
	req := provider.Request{Model: l.model, Messages: rebuilt, Tools: surface.defs, ReasoningEffort: l.defaultEffort}
	// The rebuilt history lost the dropped turns' blocks.
	req.DisableProviderReplay = true
	if rerr := l.reserveWork(ctx, req, iteration+1); rerr != nil {
		return provider.Response{}, nil, provider.Request{}, rerr
	}
	if oerr := l.observeRequest(ctx, req, iteration+1); oerr != nil {
		l.refundWork(ctx, req)
		return provider.Response{}, nil, provider.Request{}, oerr
	}
	resp, err := l.completer.Chat(ctx, req)
	if err != nil {
		l.refundWork(ctx, req)
		return provider.Response{}, nil, provider.Request{}, err
	}
	return resp, rebuilt, req, nil
}

// historyRewritten reports whether a planning pass changed the
// history: a different length, or any message whose content differs.
// It compares by value, so a pass that reorders or edits one turn
// counts as a rewrite.
func historyRewritten(before, after []provider.Message) bool {
	if len(before) != len(after) {
		return true
	}
	for i := range before {
		if !messagesEqual(before[i], after[i]) {
			return true
		}
	}
	return false
}

// messagesEqual compares two messages field by field. ReasoningBlocks
// compares element-wise in order.
func messagesEqual(a, b provider.Message) bool {
	if a.Role != b.Role || a.Content != b.Content || a.Name != b.Name ||
		a.ToolCallID != b.ToolCallID || len(a.ToolCalls) != len(b.ToolCalls) ||
		len(a.ReasoningBlocks) != len(b.ReasoningBlocks) {
		return false
	}
	for i := range a.ToolCalls {
		if a.ToolCalls[i].Index != b.ToolCalls[i].Index || a.ToolCalls[i].ID != b.ToolCalls[i].ID ||
			a.ToolCalls[i].Name != b.ToolCalls[i].Name || string(a.ToolCalls[i].Arguments) != string(b.ToolCalls[i].Arguments) {
			return false
		}
	}
	for i := range a.ReasoningBlocks {
		if a.ReasoningBlocks[i] != b.ReasoningBlocks[i] {
			return false
		}
	}
	return true
}

// EnableCompaction fills a Options' Window, Summarizer, and
// Calibrated fields from one Completer, in one call. The Completer
// must also implement provider.TokenEstimator; anthropic.Client
// does. window keeps the caller's configured trigger and target
// percentages. alpha is the calibration factor passed to
// plan.Calibrate. The Options must not already carry Trim;
// Window and Trim are mutually exclusive, and Validate rejects the
// pair. EnableCompaction and plan.NewSummarizer are the
// only sanctioned constructors for Options.Summarizer. A typed nil
// stored by hand is not nil as an interface; see the Summarizer
// interface for the warning.
func EnableCompaction(o *Options, completer provider.Completer, window plan.Window, alpha float64) error {
	est, ok := completer.(provider.TokenEstimator)
	if !ok {
		return ErrNoTokenEstimator
	}
	summarizer, err := plan.NewSummarizer(completer)
	if err != nil {
		return err
	}
	w := window
	o.Window = &w
	o.Summarizer = summarizer
	o.Calibrated = plan.Calibrate(est, alpha)
	return nil
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
